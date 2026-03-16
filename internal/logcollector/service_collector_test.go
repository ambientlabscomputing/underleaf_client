package logcollector

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/runner"
)

// ─── Mocks ────────────────────────────────────────────────────────────────────

type mockLogFetcher struct {
	entries []runner.ContainerLogEntry
	err     error
}

func (m *mockLogFetcher) GetDeploymentContainerLogs(_ context.Context, _, _ string, _ int) ([]runner.ContainerLogEntry, error) {
	return m.entries, m.err
}

type mockServiceUploader struct {
	uploadedData []byte
	uploadErr    error
	called       bool
}

func (m *mockServiceUploader) UploadServiceLogs(_ context.Context, _, _ string, data []byte) error {
	m.called = true
	m.uploadedData = data
	return m.uploadErr
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// openZip opens a zip archive from raw bytes and returns a map of
// filename -> file content.
func openZip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}
	out := make(map[string]string, len(r.File))
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("failed to open zip entry %s: %v", f.Name, err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("failed to read zip entry %s: %v", f.Name, err)
		}
		rc.Close()
		out[f.Name] = buf.String()
	}
	return out
}

// fileKeys returns the keys of a map[string]string for error messages.
func fileKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// newCmd builds a minimal ServiceLogCollectionCommand for tests.
func newCmd(slug, service string, lines int) ServiceLogCollectionCommand {
	return ServiceLogCollectionCommand{
		ServerID:       "srv-1",
		TraceID:        "trace-abc",
		DeploymentID:   "dep-1",
		DeploymentSlug: slug,
		ServiceName:    service,
		Lines:          lines,
	}
}

// ─── buildServiceZip tests ────────────────────────────────────────────────────

func TestBuildServiceZip_SingleEntry(t *testing.T) {
	entries := []runner.ContainerLogEntry{
		{ContainerName: "myapp_web", Logs: []byte("line one\nline two\n")},
	}
	data, err := buildServiceZip(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	files := openZip(t, data)
	if len(files) != 1 {
		t.Fatalf("want 1 zip entry, got %d", len(files))
	}
	content, ok := files["myapp_web.log"]
	if !ok {
		t.Fatalf("expected zip entry myapp_web.log, got keys %v", fileKeys(files))
	}
	if content != "line one\nline two\n" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestBuildServiceZip_MultipleEntries(t *testing.T) {
	entries := []runner.ContainerLogEntry{
		{ContainerName: "myapp_web", Logs: []byte("web log\n")},
		{ContainerName: "myapp_db", Logs: []byte("db log\n")},
	}
	data, err := buildServiceZip(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	files := openZip(t, data)
	if len(files) != 2 {
		t.Fatalf("want 2 zip entries, got %d", len(files))
	}
	if files["myapp_web.log"] != "web log\n" {
		t.Errorf("unexpected web log: %q", files["myapp_web.log"])
	}
	if files["myapp_db.log"] != "db log\n" {
		t.Errorf("unexpected db log: %q", files["myapp_db.log"])
	}
}

func TestBuildServiceZip_Empty(t *testing.T) {
	data, err := buildServiceZip(nil)
	if err != nil {
		t.Fatalf("unexpected error building empty zip: %v", err)
	}
	files := openZip(t, data)
	if len(files) != 0 {
		t.Fatalf("want 0 zip entries, got %d", len(files))
	}
}

// ─── collectAndUpload tests ───────────────────────────────────────────────────

func TestCollectAndUpload_SingleService(t *testing.T) {
	fetcher := &mockLogFetcher{
		entries: []runner.ContainerLogEntry{
			{ContainerName: "myapp_web", Logs: []byte("2024-01-15T10:00:00.000Z hello\n")},
		},
	}
	uploader := &mockServiceUploader{}
	c := NewServiceCollector(fetcher, uploader)

	err := c.collectAndUpload(context.Background(), newCmd("myapp", "web", 100))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !uploader.called {
		t.Fatal("expected uploader to be called")
	}
	files := openZip(t, uploader.uploadedData)
	if len(files) != 1 {
		t.Fatalf("want 1 zip entry, got %d", len(files))
	}
	if _, ok := files["myapp_web.log"]; !ok {
		t.Errorf("expected myapp_web.log in zip, got keys %v", fileKeys(files))
	}
}

func TestCollectAndUpload_AllServices(t *testing.T) {
	fetcher := &mockLogFetcher{
		entries: []runner.ContainerLogEntry{
			{ContainerName: "myapp_web", Logs: []byte("web log\n")},
			{ContainerName: "myapp_db", Logs: []byte("db log\n")},
		},
	}
	uploader := &mockServiceUploader{}
	c := NewServiceCollector(fetcher, uploader)

	err := c.collectAndUpload(context.Background(), newCmd("myapp", "", 100))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	files := openZip(t, uploader.uploadedData)
	if len(files) != 2 {
		t.Fatalf("want 2 zip entries, got %d", len(files))
	}
	if files["myapp_web.log"] != "web log\n" {
		t.Errorf("unexpected web log: %q", files["myapp_web.log"])
	}
	if files["myapp_db.log"] != "db log\n" {
		t.Errorf("unexpected db log: %q", files["myapp_db.log"])
	}
}

func TestCollectAndUpload_ZipStructure(t *testing.T) {
	logContent := "2024-01-15T10:00:00.000Z api log line\n2024-01-15T10:00:01.000Z second line\n"
	fetcher := &mockLogFetcher{
		entries: []runner.ContainerLogEntry{
			{ContainerName: "myapp_api", Logs: []byte(logContent)},
		},
	}
	uploader := &mockServiceUploader{}
	c := NewServiceCollector(fetcher, uploader)

	if err := c.collectAndUpload(context.Background(), newCmd("myapp", "api", 50)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	files := openZip(t, uploader.uploadedData)
	if got := files["myapp_api.log"]; got != logContent {
		t.Errorf("zip content mismatch: want %q, got %q", logContent, got)
	}
}

func TestCollectAndUpload_EmptyContainers(t *testing.T) {
	fetcher := &mockLogFetcher{entries: []runner.ContainerLogEntry{}}
	uploader := &mockServiceUploader{}
	c := NewServiceCollector(fetcher, uploader)

	err := c.collectAndUpload(context.Background(), newCmd("myapp", "", 100))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !uploader.called {
		t.Fatal("expected uploader to be called even for empty result")
	}
	files := openZip(t, uploader.uploadedData)
	if len(files) != 0 {
		t.Fatalf("expected empty zip, got %d entries", len(files))
	}
}

func TestCollectAndUpload_FetcherError(t *testing.T) {
	fetcher := &mockLogFetcher{err: errors.New("docker unavailable")}
	uploader := &mockServiceUploader{}
	c := NewServiceCollector(fetcher, uploader)

	err := c.collectAndUpload(context.Background(), newCmd("myapp", "", 100))
	if err == nil {
		t.Fatal("expected error from collectAndUpload, got nil")
	}
	if uploader.called {
		t.Error("uploader must not be called when fetch fails")
	}
}

func TestCollectAndUpload_UploaderError(t *testing.T) {
	fetcher := &mockLogFetcher{
		entries: []runner.ContainerLogEntry{
			{ContainerName: "myapp_web", Logs: []byte("log\n")},
		},
	}
	uploader := &mockServiceUploader{uploadErr: errors.New("network timeout")}
	c := NewServiceCollector(fetcher, uploader)

	err := c.collectAndUpload(context.Background(), newCmd("myapp", "", 100))
	if err == nil {
		t.Fatal("expected error propagated from uploader, got nil")
	}
}

// ─── HandleEvent tests ────────────────────────────────────────────────────────

func TestHandleEvent_HappyPath(t *testing.T) {
	fetcher := &mockLogFetcher{
		entries: []runner.ContainerLogEntry{
			{ContainerName: "myapp_web", Logs: []byte("hello\n")},
		},
	}
	uploader := &mockServiceUploader{}
	c := NewServiceCollector(fetcher, uploader)

	payload := []byte("{\"server_id\":\"srv-1\",\"trace_id\":\"trace-xyz\",\"deployment_id\":\"dep-1\",\"deployment_slug\":\"myapp\",\"service_name\":\"web\",\"lines\":100}")
	c.HandleEvent(context.Background(), payload)

	if !uploader.called {
		t.Fatal("expected uploader to be called after HandleEvent")
	}
}

func TestHandleEvent_ParseError(t *testing.T) {
	fetcher := &mockLogFetcher{}
	uploader := &mockServiceUploader{}
	c := NewServiceCollector(fetcher, uploader)

	c.HandleEvent(context.Background(), []byte("not valid json {{{"))

	if uploader.called {
		t.Error("uploader must not be called when payload is unparseable")
	}
}

func TestHandleEvent_DefaultLines(t *testing.T) {
	fetcher := &mockLogFetcher{entries: []runner.ContainerLogEntry{}}
	uploader := &mockServiceUploader{}
	c := NewServiceCollector(fetcher, uploader)

	payload := []byte("{\"server_id\":\"s\",\"trace_id\":\"t\",\"deployment_slug\":\"app\",\"lines\":0}")
	c.HandleEvent(context.Background(), payload)

	if !uploader.called {
		t.Fatal("expected uploader to be called")
	}
}
