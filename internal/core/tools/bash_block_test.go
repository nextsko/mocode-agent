package tools

import "testing"

func TestCatastrophicCommandBlocker(t *testing.T) {
	blocker := catastrophicCommandBlocker()

	cases := []struct {
		name    string
		args    []string
		blocked bool
	}{
		// Must block: catastrophic deletions of root / system dirs.
		{"rm -rf /", []string{"rm", "-rf", "/"}, true},
		{"rm -fr /", []string{"rm", "-fr", "/"}, true},
		{"rm -rf /*", []string{"rm", "-rf", "/*"}, true},
		{"rm -rf //", []string{"rm", "-rf", "//"}, true},
		{"rm -rf /etc", []string{"rm", "-rf", "/etc"}, true},
		{"rm -rf /usr/*", []string{"rm", "-rf", "/usr/*"}, true},
		{"rm -r -f /", []string{"rm", "-r", "-f", "/"}, true},
		{"rm --recursive --force /", []string{"rm", "--recursive", "--force", "/"}, true},
		{"rm -rfv /var", []string{"rm", "-rfv", "/var"}, true},

		// Must allow: ordinary deletions.
		{"rm -rf /tmp/build", []string{"rm", "-rf", "/tmp/build"}, false},
		{"rm -rf ./node_modules", []string{"rm", "-rf", "./node_modules"}, false},
		{"rm -rf /home/user/cache", []string{"rm", "-rf", "/home/user/cache"}, false},
		{"rm -rf /etc/nginx", []string{"rm", "-rf", "/etc/nginx"}, false},
		{"rm file.txt", []string{"rm", "file.txt"}, false},
		{"rm -r dir", []string{"rm", "-r", "dir"}, false},

		// Must block: fork bombs, mkfs.*, raw-disk dd.
		{"fork bomb", []string{":(){:|:&};:"}, true},
		{"mkfs.ext4", []string{"mkfs.ext4", "/dev/sda1"}, true},
		{"dd to raw disk", []string{"dd", "if=/dev/urandom", "of=/dev/sda"}, true},
		{"dd to nvme", []string{"dd", "if=img.iso", "of=/dev/nvme0n1"}, true},

		// Must allow: normal dd usage (not writing raw disks).
		{"dd to file", []string{"dd", "if=/dev/zero", "of=/tmp/blob", "bs=1k", "count=1"}, false},

		// Must allow: relaxed commands that used to be banned.
		{"curl", []string{"curl", "https://example.com"}, false},
		{"wget", []string{"wget", "https://example.com"}, false},
		{"sudo", []string{"sudo", "ls"}, false},
		{"apt-get install", []string{"apt-get", "install", "-y", "jq"}, false},
		{"npm install -g", []string{"npm", "install", "-g", "typescript"}, false},
		{"pip install --user", []string{"pip", "install", "--user", "rich"}, false},
		{"systemctl status", []string{"systemctl", "status", "nginx"}, false},
		{"git push", []string{"git", "push"}, false},
	}

	for _, c := range cases {
		if got := blocker(c.args); got != c.blocked {
			t.Errorf("%s: blocked = %v, want %v", c.name, got, c.blocked)
		}
	}
}

func TestBlockFuncsBannedCommands(t *testing.T) {
	for _, fn := range blockFuncs() {
		if fn([]string{"shutdown", "-h", "now"}) {
			return // blocked as expected
		}
	}
	t.Error("expected 'shutdown' to be blocked by blockFuncs()")
}
