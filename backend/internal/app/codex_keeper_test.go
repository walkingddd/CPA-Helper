package app

import "testing"

func TestKeeperDaemonSkipsWhenManualRunIsRunning(t *testing.T) {
	runner := NewKeeperRunner(nil)

	if !runner.markRunning("once") {
		t.Fatal("markRunning once = false, want true")
	}
	if runner.markRunning("daemon") {
		t.Fatal("markRunning daemon = true, want false while manual run is running")
	}

	status := runner.Status()
	if !status.Running {
		t.Fatal("status.Running = false, want true")
	}
	if status.Mode == nil || *status.Mode != "once" {
		t.Fatalf("status.Mode = %v, want once", status.Mode)
	}
}

func TestKeeperDaemonSkipsWhenAccountRefreshIsRunning(t *testing.T) {
	runner := NewKeeperRunner(nil)

	if !runner.markRunning("accounts") {
		t.Fatal("markRunning accounts = false, want true")
	}
	if runner.markRunning("daemon") {
		t.Fatal("markRunning daemon = true, want false while account refresh is running")
	}

	status := runner.Status()
	if !status.Running {
		t.Fatal("status.Running = false, want true")
	}
	if status.Mode == nil || *status.Mode != "accounts" {
		t.Fatalf("status.Mode = %v, want accounts", status.Mode)
	}
}
