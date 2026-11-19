package shell

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBashGuidanceRepeatWarning(t *testing.T) {
	t.Parallel()

	sess := "sess-repeat-" + t.Name()
	cmd := "echo repeat-probe"

	require.Empty(t, bashGuidance(cmd, recordCommandRun(sess, cmd)))
	require.Empty(t, bashGuidance(cmd, recordCommandRun(sess, cmd)))

	out := bashGuidance(cmd, recordCommandRun(sess, cmd)) // 3rd run
	require.Contains(t, out, "has now run 3 times")
	require.Contains(t, out, "[mocode]")
}

func TestBashGuidanceRepeatCountEscalates(t *testing.T) {
	t.Parallel()

	sess := "sess-repeat-esc-" + t.Name()
	cmd := "echo escalate-probe"
	for i := 1; i <= 5; i++ {
		n := recordCommandRun(sess, cmd)
		g := bashGuidance(cmd, n)
		if i < repeatWarnThreshold {
			require.Empty(t, g, "run %d must not warn", i)
			continue
		}
		require.Contains(t, g, fmt.Sprintf("has now run %d times", i))
	}
}

func TestBashGuidanceHeredocHint(t *testing.T) {
	t.Parallel()

	cases := []string{
		"cd /tmp && python3 - <<'PY'\nprint(1)\nPY",
		"python - <<EOF\nprint(1)\nEOF",
		"node --input-type=module - <<'JS'\nconsole.log(1)\nJS",
	}
	for _, cmd := range cases {
		require.Contains(t, bashGuidance(cmd, 1), "heredoc script was fed")
	}

	require.Empty(t, bashGuidance("echo plain", 1))
	// cat reading stdin via heredoc is not an interpreter script.
	require.Empty(t, bashGuidance("cat <<'EOF'\nhello\nEOF", 1))
}

func TestBashGuidanceIndependentKeys(t *testing.T) {
	t.Parallel()

	sess := "sess-repeat-iso-" + t.Name()
	a := recordCommandRun(sess, "echo A-probe")
	b := recordCommandRun(sess, "echo B-probe")
	b2 := recordCommandRun(sess, "echo B-probe")
	require.Equal(t, 1, a)
	require.Equal(t, 1, b)
	require.Equal(t, 2, b2)
	require.Empty(t, bashGuidance("echo A-probe", a))
}
