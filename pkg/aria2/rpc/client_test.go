package rpc

import (
	"context"
	"testing"
	"time"
)

// testAPIEndpoints runs tests on all API endpoints for the given client
func testAPIEndpoints(t *testing.T, rpc Client, gid string) {
	testCases := []struct {
		name string
		test func() error
	}{
		{"TellActive", func() error { _, err := rpc.TellActive(); return err }},
		{"PauseAll", func() error { _, err := rpc.PauseAll(); return err }},
		{"TellStatus", func() error { _, err := rpc.TellStatus(gid); return err }},
		{"GetURIs", func() error { _, err := rpc.GetURIs(gid); return err }},
		{"GetFiles", func() error { _, err := rpc.GetFiles(gid); return err }},
		{"GetPeers", func() error { _, err := rpc.GetPeers(gid); return err }},
		{"TellWaiting", func() error { _, err := rpc.TellWaiting(0, 1); return err }},
		{"TellStopped", func() error { _, err := rpc.TellStopped(0, 1); return err }},
		{"GetOption", func() error { _, err := rpc.GetOption(gid); return err }},
		{"GetGlobalOption", func() error { _, err := rpc.GetGlobalOption(); return err }},
		{"GetGlobalStat", func() error { _, err := rpc.GetGlobalStat(); return err }},
		{"GetSessionInfo", func() error { _, err := rpc.GetSessionInfo(); return err }},
		{"Remove", func() error { _, err := rpc.Remove(gid); return err }},
		{"FinalTellActive", func() error { _, err := rpc.TellActive(); return err }},
	}

	for _, tc := range testCases {
		if err := tc.test(); err != nil {
			t.Errorf("%s failed: %v", tc.name, err)
		}
	}
}

func TestHTTPAll(t *testing.T) {
	const targetURL = "https://nodejs.org/dist/index.json"
	rpc, err := New(context.Background(), "http://localhost:6800/jsonrpc", "", time.Second, &DummyNotifier{})
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()

	gid, err := rpc.AddURI([]string{targetURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Download GID: %s", gid)

	testAPIEndpoints(t, rpc, gid)
}

func TestWebsocketAll(t *testing.T) {
	const targetURL = "https://nodejs.org/dist/index.json"
	rpc, err := New(context.Background(), "ws://localhost:6800/jsonrpc", "", time.Second, &DummyNotifier{})
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()

	gid, err := rpc.AddURI([]string{targetURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Download GID: %s", gid)

	testAPIEndpoints(t, rpc, gid)
}
