package gps

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"testing"
)

var basicResult solution
var kub atom

func pi(n string) ProjectIdentifier {
	return ProjectIdentifier{
		ProjectRoot: ProjectRoot(n),
	}
}

func init() {
	basicResult = solution{
		att: 1,
		p: []LockedProject{
			pa2lp(atom{
				id: pi("github.com/sdboyer/testrepo"),
				v:  NewBranch("master").Is(Revision("4d59fb584b15a94d7401e356d2875c472d76ef45")),
			}, nil),
			pa2lp(atom{
				id: pi("github.com/Masterminds/VCSTestRepo"),
				v:  NewVersion("1.0.0").Is(Revision("30605f6ac35fcb075ad0bfa9296f90a7d891523e")),
			}, nil),
		},
	}

	// just in case something needs punishing, kubernetes is happy to oblige
	kub = atom{
		id: pi("github.com/kubernetes/kubernetes"),
		v:  NewVersion("1.0.0").Is(Revision("528f879e7d3790ea4287687ef0ab3f2a01cc2718")),
	}
}

func TestWriteDepTree(t *testing.T) {
	// This test is a bit slow, skip it on -short
	if testing.Short() {
		t.Skip("Skipping dep tree writing test in short mode")
	}
	requiresBins(t, "git", "hg", "bzr")

	tmp, err := ioutil.TempDir("", "writetree")
	if err != nil {
		t.Errorf("Failed to create temp dir: %s", err)
		t.FailNow()
	}
	defer os.RemoveAll(tmp)

	r := solution{
		att: 1,
		p: []LockedProject{
			pa2lp(atom{
				id: pi("github.com/sdboyer/testrepo"),
				v:  NewBranch("master").Is(Revision("4d59fb584b15a94d7401e356d2875c472d76ef45")),
			}, nil),
			pa2lp(atom{
				id: pi("launchpad.net/govcstestbzrrepo"),
				v:  NewVersion("1.0.0").Is(Revision("matt@mattfarina.com-20150731135137-pbphasfppmygpl68")),
			}, nil),
			pa2lp(atom{
				id: pi("bitbucket.org/sdboyer/withbm"),
				v:  NewVersion("v1.0.0").Is(Revision("aa110802a0c64195d0a6c375c9f66668827c90b4")),
			}, nil),
		},
	}

	sm, clean := mkNaiveSM(t)
	defer clean()

	// nil lock/result should err immediately
	err = WriteDepTree(tmp, nil, sm, true)
	if err == nil {
		t.Errorf("Should error if nil lock is passed to WriteDepTree")
	}

	err = WriteDepTree(tmp, r, sm, true)
	if err != nil {
		t.Errorf("Unexpected error while creating vendor tree: %s", err)
	}

	if _, err = os.Stat(filepath.Join(tmp, "github.com", "sdboyer", "testrepo")); err != nil {
		t.Errorf("Directory for github.com/sdboyer/testrepo does not exist")
	}
	if _, err = os.Stat(filepath.Join(tmp, "launchpad.net", "govcstestbzrrepo")); err != nil {
		t.Errorf("Directory for launchpad.net/govcstestbzrrepo does not exist")
	}
	if _, err = os.Stat(filepath.Join(tmp, "bitbucket.org", "sdboyer", "withbm")); err != nil {
		t.Errorf("Directory for bitbucket.org/sdboyer/withbm does not exist")
	}
}

func BenchmarkCreateVendorTree(b *testing.B) {
	// We're fs-bound here, so restrict to single parallelism
	b.SetParallelism(1)

	r := basicResult
	tmp := path.Join(os.TempDir(), "vsolvtest")

	clean := true
	sm, err := NewSourceManager(naiveAnalyzer{}, path.Join(tmp, "cache"))
	if err != nil {
		b.Errorf("NewSourceManager errored unexpectedly: %q", err)
		clean = false
	}

	// Prefetch the projects before timer starts
	for _, lp := range r.p {
		err := sm.SyncSourceFor(lp.Ident())
		if err != nil {
			b.Errorf("failed getting project info during prefetch: %s", err)
			clean = false
		}
	}

	if clean {
		b.ResetTimer()
		b.StopTimer()
		exp := path.Join(tmp, "export")
		for i := 0; i < b.N; i++ {
			// Order the loop this way to make it easy to disable final cleanup, to
			// ease manual inspection
			os.RemoveAll(exp)
			b.StartTimer()
			err = WriteDepTree(exp, r, sm, true)
			b.StopTimer()
			if err != nil {
				b.Errorf("unexpected error after %v iterations: %s", i, err)
				break
			}
		}
	}

	sm.Release()
	os.RemoveAll(tmp) // comment this to leave temp dir behind for inspection
}

func TestStripVendor(t *testing.T) {
	type testcase struct {
		before, after filesystemState
	}

	test := func(tc testcase) func(*testing.T) {
		return func(t *testing.T) {
			tempDir, err := ioutil.TempDir("", "TestStripVendor")
			if err != nil {
				t.Fatalf("ioutil.TempDir err=%q", err)
			}
			defer func() {
				if err := os.RemoveAll(tempDir); err != nil {
					t.Errorf("os.RemoveAll(%q) err=%q", tempDir, err)
				}
			}()
			tc.before.root = tempDir
			tc.after.root = tempDir

			tc.before.setup(t)

			if err := filepath.Walk(tempDir, stripVendor); err != nil {
				t.Errorf("filepath.Walk err=%q", err)
			}

			tc.after.assert(t)
		}
	}

	t.Run("vendor directory", test(testcase{
		before: filesystemState{
			dirs: []fsPath{
				fsPath{"package"},
				fsPath{"package", "vendor"},
			},
		},
		after: filesystemState{
			dirs: []fsPath{
				fsPath{"package"},
			},
		},
	}))

	t.Run("vendor file", test(testcase{
		before: filesystemState{
			dirs: []fsPath{
				fsPath{"package"},
			},
			files: []fsPath{
				fsPath{"package", "vendor"},
			},
		},
		after: filesystemState{
			dirs: []fsPath{
				fsPath{"package"},
			},
			files: []fsPath{
				fsPath{"package", "vendor"},
			},
		},
	}))

	t.Run("vendor symlink", test(testcase{
		before: filesystemState{
			dirs: []fsPath{
				fsPath{"package"},
				fsPath{"package", "_vendor"},
			},
			links: []fsLink{
				fsLink{
					path: fsPath{"package", "vendor"},
					to:   "_vendor",
				},
			},
		},
		after: filesystemState{
			dirs: []fsPath{
				fsPath{"package"},
				fsPath{"package", "_vendor"},
			},
		},
	}))

	t.Run("nonvendor symlink", test(testcase{
		before: filesystemState{
			dirs: []fsPath{
				fsPath{"package"},
				fsPath{"package", "_vendor"},
			},
			links: []fsLink{
				fsLink{
					path: fsPath{"package", "link"},
					to:   "_vendor",
				},
			},
		},
		after: filesystemState{
			dirs: []fsPath{
				fsPath{"package"},
				fsPath{"package", "_vendor"},
			},
			links: []fsLink{
				fsLink{
					path: fsPath{"package", "link"},
					to:   "_vendor",
				},
			},
		},
	}))

	t.Run("vendor symlink to file", test(testcase{
		before: filesystemState{
			files: []fsPath{
				fsPath{"file"},
			},
			links: []fsLink{
				fsLink{
					path: fsPath{"vendor"},
					to:   "file",
				},
			},
		},
		after: filesystemState{
			files: []fsPath{
				fsPath{"file"},
			},
			links: []fsLink{
				fsLink{
					path: fsPath{"vendor"},
					to:   "file",
				},
			},
		},
	}))

	t.Run("chained symlinks", test(testcase{
		before: filesystemState{
			dirs: []fsPath{
				fsPath{"_vendor"},
			},
			links: []fsLink{
				fsLink{
					path: fsPath{"vendor"},
					to:   "vendor2",
				},
				fsLink{
					path: fsPath{"vendor2"},
					to:   "_vendor",
				},
			},
		},
		after: filesystemState{
			dirs: []fsPath{
				fsPath{"_vendor"},
			},
			links: []fsLink{
				fsLink{
					path: fsPath{"vendor2"},
					to:   "_vendor",
				},
			},
		},
	}))

	t.Run("circular symlinks", test(testcase{
		before: filesystemState{
			dirs: []fsPath{
				fsPath{"package"},
			},
			links: []fsLink{
				fsLink{
					path: fsPath{"package", "link1"},
					to:   "link2",
				},
				fsLink{
					path: fsPath{"package", "link2"},
					to:   "link1",
				},
			},
		},
		after: filesystemState{
			dirs: []fsPath{
				fsPath{"package"},
			},
			links: []fsLink{
				fsLink{
					path: fsPath{"package", "link1"},
					to:   "link2",
				},
				fsLink{
					path: fsPath{"package", "link2"},
					to:   "link1",
				},
			},
		},
	}))

}
