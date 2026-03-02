package servers

import (
	"context"
	"errors"
	"testing"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
)

// --- mock serverLookup ---

type mockServerLookup struct {
	getServerFn             func(ctx context.Context, serverID string) (servertypes.Server, error)
	listServersWithParamsFn func(ctx context.Context, params servertypes.ListServersParams) ([]servertypes.Server, error)
}

func (m *mockServerLookup) GetServer(ctx context.Context, serverID string) (servertypes.Server, error) {
	return m.getServerFn(ctx, serverID)
}

func (m *mockServerLookup) ListServersWithParams(ctx context.Context, params servertypes.ListServersParams) ([]servertypes.Server, error) {
	return m.listServersWithParamsFn(ctx, params)
}

// --- parseExecArgs tests ---

func TestParseExecArgs_WithSeparator(t *testing.T) {
	sel, cmd, err := parseExecArgs([]string{"my-server", "--", "echo", "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel != "my-server" {
		t.Errorf("selector = %q, want my-server", sel)
	}
	if len(cmd) != 2 || cmd[0] != "echo" || cmd[1] != "hello" {
		t.Errorf("command = %v, want [echo hello]", cmd)
	}
}

func TestParseExecArgs_WithoutSeparator(t *testing.T) {
	sel, cmd, err := parseExecArgs([]string{"my-server", "uptime"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel != "my-server" {
		t.Errorf("selector = %q, want my-server", sel)
	}
	if len(cmd) != 1 || cmd[0] != "uptime" {
		t.Errorf("command = %v, want [uptime]", cmd)
	}
}

func TestParseExecArgs_MissingSelectorBeforeSeparator(t *testing.T) {
	_, _, err := parseExecArgs([]string{"--", "echo"})
	if err == nil {
		t.Error("expected error for missing selector before --")
	}
}

func TestParseExecArgs_MissingCommandAfterSeparator(t *testing.T) {
	_, _, err := parseExecArgs([]string{"my-server", "--"})
	if err == nil {
		t.Error("expected error for missing command after --")
	}
}

func TestParseExecArgs_TooFewArguments(t *testing.T) {
	_, _, err := parseExecArgs([]string{"my-server"})
	if err == nil {
		t.Error("expected error when only selector is provided with no separator or command")
	}
}

// --- resolveServerID tests ---

func TestResolveServerID_FoundByID(t *testing.T) {
	svc := &mockServerLookup{
		getServerFn: func(_ context.Context, serverID string) (servertypes.Server, error) {
			return servertypes.Server{ID: "uuid-abc", Name: "prod-server"}, nil
		},
	}
	id, err := resolveServerID(context.Background(), svc, "uuid-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "uuid-abc" {
		t.Errorf("id = %q, want uuid-abc", id)
	}
}

func TestResolveServerID_FoundByName(t *testing.T) {
	svc := &mockServerLookup{
		getServerFn: func(_ context.Context, serverID string) (servertypes.Server, error) {
			return servertypes.Server{}, errors.New("not found")
		},
		listServersWithParamsFn: func(_ context.Context, params servertypes.ListServersParams) ([]servertypes.Server, error) {
			if params.Search != "prod-server" {
				return nil, nil
			}
			return []servertypes.Server{
				{ID: "uuid-abc", Name: "other-server"},
				{ID: "uuid-xyz", Name: "prod-server"},
			}, nil
		},
	}
	id, err := resolveServerID(context.Background(), svc, "prod-server")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "uuid-xyz" {
		t.Errorf("id = %q, want uuid-xyz", id)
	}
}

func TestResolveServerID_NameMatchIsExact(t *testing.T) {
	svc := &mockServerLookup{
		getServerFn: func(_ context.Context, _ string) (servertypes.Server, error) {
			return servertypes.Server{}, errors.New("not found")
		},
		listServersWithParamsFn: func(_ context.Context, _ servertypes.ListServersParams) ([]servertypes.Server, error) {
			return []servertypes.Server{{ID: "uuid-xyz", Name: "prod-server"}}, nil
		},
	}
	_, err := resolveServerID(context.Background(), svc, "prod")
	if err == nil {
		t.Error("expected error for partial name match")
	}
}

func TestResolveServerID_NotFound(t *testing.T) {
	svc := &mockServerLookup{
		getServerFn: func(_ context.Context, _ string) (servertypes.Server, error) {
			return servertypes.Server{}, errors.New("not found")
		},
		listServersWithParamsFn: func(_ context.Context, _ servertypes.ListServersParams) ([]servertypes.Server, error) {
			return []servertypes.Server{}, nil
		},
	}
	_, err := resolveServerID(context.Background(), svc, "ghost-server")
	if err == nil {
		t.Error("expected error when no server matches")
	}
}

func TestResolveServerID_ListError(t *testing.T) {
	svc := &mockServerLookup{
		getServerFn: func(_ context.Context, _ string) (servertypes.Server, error) {
			return servertypes.Server{}, errors.New("not found")
		},
		listServersWithParamsFn: func(_ context.Context, _ servertypes.ListServersParams) ([]servertypes.Server, error) {
			return nil, errors.New("api error")
		},
	}
	_, err := resolveServerID(context.Background(), svc, "prod-server")
	if err == nil {
		t.Error("expected error when list fails")
	}
}

// --- applyCommandSelector tests (non-server-lookup paths) ---

func TestApplyCommandSelector_All(t *testing.T) {
	req := &controlplane.DispatchCommandRequest{}
	err := applyCommandSelector(context.Background(), nil, req, "all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !req.AllServers {
		t.Error("expected AllServers = true")
	}
}

func TestApplyCommandSelector_TagSelector(t *testing.T) {
	req := &controlplane.DispatchCommandRequest{}
	err := applyCommandSelector(context.Background(), nil, req, "env=production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Tags["env"] != "production" {
		t.Errorf("tag env = %q, want production", req.Tags["env"])
	}
}

func TestApplyCommandSelector_TagSelectorWithEqualsInValue(t *testing.T) {
	req := &controlplane.DispatchCommandRequest{}
	err := applyCommandSelector(context.Background(), nil, req, "label=a=b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Tags["label"] != "a=b" {
		t.Errorf("tag label = %q, want a=b", req.Tags["label"])
	}
}
