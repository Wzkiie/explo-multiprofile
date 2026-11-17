package run

import (
	"errors"
	"testing"

	"explo/src/web/backend/app"
)

func TestClosedRunRefusesAndReleasesSlot(t *testing.T) {
	mr := NewManualRun(app.Config{ExploPath: "/bin/true", WebDataDir: t.TempDir()})
	if !mr.Close() {
		t.Fatal("close failed")
	}
	if err := mr.startRun(nil); !errors.Is(err, ErrProfileClosed) {
		t.Fatalf("expected ErrProfileClosed, got %v", err)
	}
	select {
	case runSlot <- struct{}{}:
		<-runSlot // slot was free, as it must be after a refused start
	default:
		t.Fatal("a refused start must not keep the global run slot")
	}
	mr.Reopen()
}
