package runner

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// ─── Mock Docker log client ──────────────────────────────────────────────────

type mockDockerLogClient struct {
	listResult client.ContainerListResult
	listErr    error

	// Per-container log data keyed by container ID.
	logsData map[string][]byte
	logsErr  map[string]error
}

func (m *mockDockerLogClient) ContainerList(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
	return m.listResult, m.listErr
}

func (m *mockDockerLogClient) ContainerLogs(_ context.Context, containerID string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	if err, ok := m.logsErr[containerID]; ok && err != nil {
		return nil, err
	}
	data := m.logsData[containerID]
	return io.NopCloser(bytes.NewReader(data)), nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// makeDemuxedFrame builds a single Docker log frame for the given payload bytes.
// Byte 0 is the stream type (1 = stdout); bytes 4-7 are the payload length.
func makeDemuxedFrame(payload []byte) []byte {
	hdr := make([]byte, 8)
	hdr[0] = 1 // stdout
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(payload)))
	return append(hdr, payload...)
}

// demuxedLogs assembles one or more framed payloads into a complete multiplexed
// log stream as the Docker daemon would produce it.
func demuxedLogs(payloads ...string) []byte {
	var buf bytes.Buffer
	for _, p := range payloads {
		buf.Write(makeDemuxedFrame([]byte(p)))
	}
	return buf.Bytes()
}

// newTestRunner creates a Runner backed by the given mock without touching the
// filesystem or Docker daemon. reportPath is irrelevant for log tests.
func newTestRunner(mock dockerLogClient) *Runner {
	return &Runner{logClient: mock, reportPath: "/tmp/test"}
}

// ─── demuxDockerLogs tests ───────────────────────────────────────────────────

func TestDemuxDockerLogs_SingleFrame(t *testing.T) {
	payload := "2024-01-15T10:00:00.000Z hello world\n"
	stream := bytes.NewReader(makeDemuxedFrame([]byte(payload)))
	got, err := demuxDockerLogs(stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != payload {
		t.Errorf("want %q, got %q", payload, string(got))
	}
}

func TestDemuxDockerLogs_MultipleFrames(t *testing.T) {
	lines := []string{
		"2024-01-15T10:00:00.000Z line one\n",
		"2024-01-15T10:00:01.000Z line two\n",
		"2024-01-15T10:00:02.000Z line three\n",
	}
	stream := bytes.NewReader(demuxedLogs(lines...))
	got, err := demuxDockerLogs(stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := lines[0] + lines[1] + lines[2]
	if string(got) != want {
		t.Errorf("want %q, got %q", want, string(got))
	}
}

func TestDemuxDockerLogs_EmptyStream(t *testing.T) {
	got, err := demuxDockerLogs(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty output, got %d bytes", len(got))
	}
}

func TestDemuxDockerLogs_ZeroSizeFrameSkipped(t *testing.T) {
	// A frame with payload size 0 should be skipped.
	zeroFrame := make([]byte, 8) // header with zero payload length
	zeroFrame[0] = 1
	payload := "2024-01-15T10:00:00.000Z real line\n"
	realFrame := makeDemuxedFrame([]byte(payload))
	stream := bytes.NewReader(append(zeroFrame, realFrame...))
	got, err := demuxDockerLogs(stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != payload {
		t.Errorf("want %q, got %q", payload, string(got))
	}
}

// ─── GetDeploymentContainerLogs tests ────────────────────────────────────────

func TestGetDeploymentContainerLogs_AllContainers(t *testing.T) {
	logDataA := demuxedLogs("2024-01-15T10:00:00.000Z web log line\n")
	logDataB := demuxedLogs("2024-01-15T10:00:01.000Z db log line\n")
	mock := &mockDockerLogClient{
		listResult: client.ContainerListResult{
			Items: []container.Summary{
				{ID: "ctr-web", Names: []string{"/myapp_web"}, Labels: map[string]string{"underleaf.slug": "myapp"}},
				{ID: "ctr-db", Names: []string{"/myapp_db"}, Labels: map[string]string{"underleaf.slug": "myapp"}},
			},
		},
		logsData: map[string][]byte{
			"ctr-web": logDataA,
			"ctr-db":  logDataB,
		},
		logsErr: map[string]error{},
	}
	r := newTestRunner(mock)
	entries, err := r.GetDeploymentContainerLogs(context.Background(), "myapp", "", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.ContainerName] = true
	}
	if !names["myapp_web"] || !names["myapp_db"] {
		t.Errorf("expected both myapp_web and myapp_db, got %v", names)
	}
}

func TestGetDeploymentContainerLogs_ServiceFilter(t *testing.T) {
	logData := demuxedLogs("2024-01-15T10:00:00.000Z api log\n")
	mock := &mockDockerLogClient{
		listResult: client.ContainerListResult{
			Items: []container.Summary{
				{ID: "ctr-api", Names: []string{"/myapp_api"}, Labels: map[string]string{"underleaf.slug": "myapp"}},
				{ID: "ctr-worker", Names: []string{"/myapp_worker"}, Labels: map[string]string{"underleaf.slug": "myapp"}},
			},
		},
		logsData: map[string][]byte{"ctr-api": logData},
		logsErr:  map[string]error{},
	}
	r := newTestRunner(mock)
	entries, err := r.GetDeploymentContainerLogs(context.Background(), "myapp", "api", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].ContainerName != "myapp_api" {
		t.Errorf("want myapp_api, got %s", entries[0].ContainerName)
	}
	if string(entries[0].Logs) != "2024-01-15T10:00:00.000Z api log\n" {
		t.Errorf("unexpected log content: %q", string(entries[0].Logs))
	}
}

func TestGetDeploymentContainerLogs_ServiceFilterHyphen(t *testing.T) {
	logData := demuxedLogs("2024-01-15T10:00:00.000Z app log\n")
	mock := &mockDockerLogClient{
		listResult: client.ContainerListResult{
			Items: []container.Summary{
				{ID: "ctr-app", Names: []string{"/hello-world-app"}, Labels: map[string]string{"underleaf.slug": "hello-world"}},
			},
		},
		logsData: map[string][]byte{"ctr-app": logData},
		logsErr:  map[string]error{},
	}
	r := newTestRunner(mock)
	entries, err := r.GetDeploymentContainerLogs(context.Background(), "hello-world", "app", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].ContainerName != "hello-world-app" {
		t.Errorf("want hello-world-app, got %s", entries[0].ContainerName)
	}
}

func TestGetDeploymentContainerLogs_FiltersWrongSlug(t *testing.T) {
	mock := &mockDockerLogClient{
		listResult: client.ContainerListResult{
			Items: []container.Summary{
				// Different slug - must be excluded.
				{ID: "ctr-other", Names: []string{"/other_api"}, Labels: map[string]string{"underleaf.slug": "other"}},
				// Our slug.
				{ID: "ctr-mine", Names: []string{"/myapp_web"}, Labels: map[string]string{"underleaf.slug": "myapp"}},
			},
		},
		logsData: map[string][]byte{"ctr-mine": demuxedLogs("mine\n")},
		logsErr:  map[string]error{},
	}
	r := newTestRunner(mock)
	entries, err := r.GetDeploymentContainerLogs(context.Background(), "myapp", "", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].ContainerName != "myapp_web" {
		t.Errorf("expected myapp_web, got %s", entries[0].ContainerName)
	}
}

func TestGetDeploymentContainerLogs_NoContainersFound(t *testing.T) {
	mock := &mockDockerLogClient{
		listResult: client.ContainerListResult{Items: []container.Summary{}},
		logsData:   map[string][]byte{},
		logsErr:    map[string]error{},
	}
	r := newTestRunner(mock)
	entries, err := r.GetDeploymentContainerLogs(context.Background(), "myapp", "", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty result, got %d entries", len(entries))
	}
}

func TestGetDeploymentContainerLogs_ContainerListError(t *testing.T) {
	mock := &mockDockerLogClient{
		listErr:  errors.New("docker daemon unavailable"),
		logsData: map[string][]byte{},
		logsErr:  map[string]error{},
	}
	r := newTestRunner(mock)
	_, err := r.GetDeploymentContainerLogs(context.Background(), "myapp", "", 100)
	if err == nil {
		t.Fatal("expected error from ContainerList, got nil")
	}
}

func TestGetDeploymentContainerLogs_LogFetchErrorFallback(t *testing.T) {
	// A single container whose log fetch fails. The function should continue
	// and return a placeholder error message rather than aborting.
	mock := &mockDockerLogClient{
		listResult: client.ContainerListResult{
			Items: []container.Summary{
				{ID: "ctr-bad", Names: []string{"/myapp_web"}, Labels: map[string]string{"underleaf.slug": "myapp"}},
			},
		},
		logsData: map[string][]byte{},
		logsErr:  map[string]error{"ctr-bad": errors.New("read timeout")},
	}
	r := newTestRunner(mock)
	entries, err := r.GetDeploymentContainerLogs(context.Background(), "myapp", "", 100)
	if err != nil {
		t.Fatalf("expected nil error (graceful fallback), got %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry (with error placeholder), got %d", len(entries))
	}
	// The entry should contain the error placeholder text.
	if !bytes.Contains(entries[0].Logs, []byte("error fetching logs")) {
		t.Errorf("expected error placeholder in logs, got: %q", string(entries[0].Logs))
	}
}

func TestGetDeploymentContainerLogs_DefaultLines(t *testing.T) {
	// Passing lines<=0 should default to 500 without panicking.
	mock := &mockDockerLogClient{
		listResult: client.ContainerListResult{Items: []container.Summary{}},
		logsData:   map[string][]byte{},
		logsErr:    map[string]error{},
	}
	r := newTestRunner(mock)
	entries, err := r.GetDeploymentContainerLogs(context.Background(), "myapp", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty result, got %d entries", len(entries))
	}
}
