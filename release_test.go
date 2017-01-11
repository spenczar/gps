package gps

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestErrAfterRelease(t *testing.T) {
	sm, clean := mkNaiveSM(t)
	clean()
	id := ProjectIdentifier{}

	_, err := sm.SourceExists(id)
	if err == nil {
		t.Errorf("SourceExists did not error after calling Release()")
	} else if terr, ok := err.(smIsReleased); !ok {
		t.Errorf("SourceExists errored after Release(), but with unexpected error: %T %s", terr, terr.Error())
	}

	err = sm.SyncSourceFor(id)
	if err == nil {
		t.Errorf("SyncSourceFor did not error after calling Release()")
	} else if terr, ok := err.(smIsReleased); !ok {
		t.Errorf("SyncSourceFor errored after Release(), but with unexpected error: %T %s", terr, terr.Error())
	}

	_, err = sm.ListVersions(id)
	if err == nil {
		t.Errorf("ListVersions did not error after calling Release()")
	} else if terr, ok := err.(smIsReleased); !ok {
		t.Errorf("ListVersions errored after Release(), but with unexpected error: %T %s", terr, terr.Error())
	}

	_, err = sm.RevisionPresentIn(id, "")
	if err == nil {
		t.Errorf("RevisionPresentIn did not error after calling Release()")
	} else if terr, ok := err.(smIsReleased); !ok {
		t.Errorf("RevisionPresentIn errored after Release(), but with unexpected error: %T %s", terr, terr.Error())
	}

	_, err = sm.ListPackages(id, nil)
	if err == nil {
		t.Errorf("ListPackages did not error after calling Release()")
	} else if terr, ok := err.(smIsReleased); !ok {
		t.Errorf("ListPackages errored after Release(), but with unexpected error: %T %s", terr, terr.Error())
	}

	_, _, err = sm.GetManifestAndLock(id, nil)
	if err == nil {
		t.Errorf("GetManifestAndLock did not error after calling Release()")
	} else if terr, ok := err.(smIsReleased); !ok {
		t.Errorf("GetManifestAndLock errored after Release(), but with unexpected error: %T %s", terr, terr.Error())
	}

	err = sm.ExportProject(id, nil, "")
	if err == nil {
		t.Errorf("ExportProject did not error after calling Release()")
	} else if terr, ok := err.(smIsReleased); !ok {
		t.Errorf("ExportProject errored after Release(), but with unexpected error: %T %s", terr, terr.Error())
	}

	_, err = sm.DeduceProjectRoot("")
	if err == nil {
		t.Errorf("DeduceProjectRoot did not error after calling Release()")
	} else if terr, ok := err.(smIsReleased); !ok {
		t.Errorf("DeduceProjectRoot errored after Release(), but with unexpected error: %T %s", terr, terr.Error())
	}
}

func TestSignalHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow test in short mode")
	}

	sm, clean := mkNaiveSM(t)
	//get self proc
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal("cannot find self proc")
	}

	sigch := make(chan os.Signal)
	sm.HandleSignals(sigch)

	sigch <- os.Interrupt
	<-time.After(10 * time.Millisecond)

	if sm.releasing != 1 {
		t.Error("Releasing flag did not get set")
	}

	lpath := filepath.Join(sm.cachedir, "sm.lock")
	if _, err := os.Stat(lpath); err == nil {
		t.Fatal("Expected error on statting what should be an absent lock file")
	}
	clean()

	sm, clean = mkNaiveSM(t)
	sm.UseDefaultSignalHandling()
	go sm.DeduceProjectRoot("rsc.io/pdf")
	runtime.Gosched()

	// signal the process and call release right afterward
	now := time.Now()
	proc.Signal(os.Interrupt)
	sigdur := time.Since(now)
	t.Logf("time to send signal: %v", sigdur)
	sm.Release()
	reldur := time.Since(now) - sigdur
	t.Logf("time to return from Release(): %v", reldur)

	if reldur < 10*time.Millisecond {
		t.Errorf("finished too fast (%v); the necessary network request could not have completed yet", reldur)
	}
	if sm.releasing != 1 {
		t.Error("Releasing flag did not get set")
	}

	lpath = filepath.Join(sm.cachedir, "sm.lock")
	if _, err := os.Stat(lpath); err == nil {
		t.Error("Expected error on statting what should be an absent lock file")
	}
	clean()

	sm, clean = mkNaiveSM(t)
	sm.UseDefaultSignalHandling()
	sm.StopSignalHandling()
	sm.UseDefaultSignalHandling()

	go sm.DeduceProjectRoot("rsc.io/pdf")
	//runtime.Gosched()
	// Ensure that it all works after teardown and re-set up
	proc.Signal(os.Interrupt)
	// Wait for twice the time it took to do it last time; should be safe
	<-time.After(reldur * 2)

	// proc.Signal doesn't send for windows, so just force it
	if runtime.GOOS == "windows" {
		sm.Release()
	}

	if sm.releasing != 1 {
		t.Error("Releasing flag did not get set")
	}

	lpath = filepath.Join(sm.cachedir, "sm.lock")
	if _, err := os.Stat(lpath); err == nil {
		t.Fatal("Expected error on statting what should be an absent lock file")
	}
	clean()
}
