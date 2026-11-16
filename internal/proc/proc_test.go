package proc

import (
	"os"
	"testing"
)

func TestOwnsRejectsADifferentCommand(t *testing.T) {
	if owns(0, "anything") {
		t.Fatal("pid 0")
	}
	if owns(os.Getpid(), "/no/such/starport-config.yaml") {
		t.Fatal("this test process must not look like our sidecar")
	}
}
