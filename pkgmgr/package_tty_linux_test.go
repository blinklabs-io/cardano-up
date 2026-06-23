// Copyright 2025 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pkgmgr

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY allocates a pseudo-terminal pair and returns its master and slave
// ends. The slave behaves like a real terminal, which is what we need to prove
// that runHookScript forwards a TTY stdin through to the hook's child process.
//
// This uses the Linux /dev/ptmx mechanism directly to avoid pulling in a PTY
// dependency just for tests. If a PTY cannot be allocated (e.g. a restricted
// sandbox without /dev/pts) the test is skipped rather than failed.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()

	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot open /dev/ptmx, skipping TTY test: %v", err)
	}
	master = os.NewFile(uintptr(fd), "ptmx")

	// Unlock the slave side before opening it.
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		t.Skipf("cannot unlock pts (TIOCSPTLCK): %v", err)
	}
	ptn, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		t.Skipf("cannot get pts number (TIOCGPTN): %v", err)
	}

	slavePath := fmt.Sprintf("/dev/pts/%d", ptn)
	slave, err = os.OpenFile(slavePath, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		t.Skipf("cannot open %s: %v", slavePath, err)
	}
	return master, slave
}

// TestRunHookScriptStdinTTY is the regression guard for issue #239: when our
// own stdin is a terminal, the hook's child process must also see its stdin as
// a terminal (so e.g. "docker run -ti" can allocate a TTY). The negative
// control confirms the check is meaningful — a non-terminal stdin must not look
// like a TTY.
func TestRunHookScriptStdinTTY(t *testing.T) {
	t.Run("tty stdin is a tty to the child", func(t *testing.T) {
		master, slave := openPTY(t)
		defer master.Close()
		defer slave.Close()

		// `test -t 0` exits 0 only if fd 0 is a terminal.
		if _, err := runHookWithStdin(t, slave, "test -t 0"); err != nil {
			t.Fatalf(
				"expected the hook's child to see a TTY stdin, got error: %v",
				err,
			)
		}
	})

	t.Run("non-tty stdin is not a tty to the child", func(t *testing.T) {
		f, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatalf("failed to open %s: %v", os.DevNull, err)
		}
		defer f.Close()

		if _, err := runHookWithStdin(t, f, "test -t 0"); err == nil {
			t.Fatal("expected non-TTY stdin to fail `test -t 0`, got nil error")
		}
	})
}
