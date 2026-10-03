package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/talvor/otman/internal/cli"
)

// claim assigns an unassigned open Item to the actor, blocked or not, and
// is a no-op with changed:false when the actor already holds it. release
// unassigns an Item the actor holds, at either status, and is a no-op on
// an unassigned Item. Both set updated only when they change something.
func TestClaimRelease(t *testing.T) {
	me := map[string]string{"OTM_ACTOR": "talvor"}
	runGolden(t, goldenCase{
		name:    "item-claim-release",
		fixture: "items",
		files: withOTM(map[string]string{
			"vault/Projects/OTM/Issues/OTM-3 Blocked by the first.md": relationItem("OTM-3", "Blocked by the first",
				"null", `["[[OTM-1 Handle sync collisions]]"]`),
			"vault/Projects/OTM/Issues/OTM-4 Closed and claimed.md": "---\nid: OTM-4\ntitle: Closed and claimed\n" +
				"kind: issue\nstatus: closed\nassignee: talvor\n---\n<!-- otman:comments -->\n## Comments\n",
		}),
		steps: []step{
			{args: []string{"claim", "WEB-1"}, env: me, tty: true},
			{args: []string{"claim", "WEB-1", "--json"}, env: me},
			{args: []string{"claim", "Projects/WEB/PRDs/WEB-1 Landing page.md"}, env: me, tty: true},
			{args: []string{"claim", "OTM-3"}, env: me},
			{args: []string{"release", "OTM-3", "--json"}, env: me},
			{args: []string{"release", "OTM-3"}, env: me, tty: true},
			{args: []string{"release", "OTM-3", "--json"}, env: me},
			{args: []string{"release", "OTM-4", "--json"}, env: me},
			{args: []string{"claim", "OTM-1", "--actor", "agent-b", "--json"}, env: me},
			{args: []string{"release", "OTM-1", "--actor", "agent-b"}, env: me, tty: true},
			{args: []string{"claim", "OTM-1", "--json"}, env: me},
		},
	})
}

// claim and release need an actor and one REF. A claim on a closed Item,
// or one with no valid status, is refused with item_not_open, and a claim
// or release of an Item someone else holds with claim_conflict; both exit
// 4 and write nothing.
func TestClaimReleaseErrors(t *testing.T) {
	me := map[string]string{"OTM_ACTOR": "talvor"}
	runGolden(t, goldenCase{
		name:    "item-claim-release-errors",
		fixture: "items",
		files: withOTM(map[string]string{
			"vault/Projects/OTM/Issues/OTM-3 No status.md": "---\nid: OTM-3\ntitle: No status\nkind: issue\n" +
				"---\n<!-- otman:comments -->\n## Comments\n",
		}),
		steps: []step{
			{args: []string{"claim", "OTM-1", "--json"}, env: me},
			{args: []string{"claim", "OTM-1"}, env: me, tty: true},
			{args: []string{"release", "OTM-1", "--json"}, env: me},
			{args: []string{"release", "OTM-1"}, env: me},
			{args: []string{"claim", "OTM-2", "--json"}, env: me},
			{args: []string{"claim", "OTM-2"}, env: me, tty: true},
			{args: []string{"claim", "OTM-3", "--json"}, env: me},
			{args: []string{"claim", "WEB-1", "--json"}},
			{args: []string{"release", "WEB-1"}, tty: true},
			{args: []string{"claim", "WEB-1", "--actor", ""}},
			{args: []string{"claim"}, env: me, tty: true},
			{args: []string{"release", "OTM-1", "OTM-2", "--json"}, env: me},
			{args: []string{"claim", "OTM-99", "--json"}, env: me},
		},
	})
}

// Claims splice only assignee and updated into hand-edited frontmatter,
// keeping its comments, quoting and line endings. Frontmatter otman cannot
// splice safely is refused with unsafe_write, even for a no-op.
func TestClaimHandEdited(t *testing.T) {
	agent := map[string]string{"OTM_ACTOR": "agent-b"}
	runGolden(t, goldenCase{
		name:    "item-claim-hand-edited",
		fixture: "handedited",
		files:   map[string]string{"config/otman/config.toml": "vault = \"$WORK/vault\"\nproject = \"HND\"\n"},
		steps: []step{
			{args: []string{"release", "HND-1", "--json"}, env: agent},
			{args: []string{"claim", "HND-2", "--json"}, env: agent},
			{args: []string{"release", "HND-3", "--json"}, env: agent},
			{args: []string{"claim", "HND-3", "--json"}, env: agent},
			{args: []string{"release", "HND-4", "--json"}, env: agent},
			{args: []string{"claim", "HND-5", "--json"}, env: agent},
			{args: []string{"claim", "HND-6", "--json"}, env: agent},
			{args: []string{"release", "HND-7", "--json"}, env: agent},
			{args: []string{"claim", "HND-10", "--json"}, env: agent},
		},
	})
}

// Concurrent claims of one Item by different actors on one device are
// serialised by the lock, which covers the check and the write: exactly
// one succeeds and the rest conflict, and the file names the winner.
func TestClaimConcurrent(t *testing.T) {
	work := t.TempDir()
	vault := filepath.Join(work, "vault")
	copyTree(t, filepath.Join("testdata", "vaults", "items"), vault)
	const n = 8
	codes := make([]int, n)
	stdouts := make([]string, n)
	stderrs := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var stdout, stderr bytes.Buffer
			codes[i] = cli.Run(cli.Options{
				Args:   []string{"claim", "WEB-1", "--json", "--vault", vault, "--actor", "agent-" + string(rune('a'+i))},
				Env:    []string{"XDG_CONFIG_HOME=" + filepath.Join(work, "config")},
				Dir:    work,
				Stdin:  strings.NewReader(""),
				Stdout: &stdout,
				Stderr: &stderr,
				Now:    func() time.Time { return fixedNow },
			})
			stdouts[i], stderrs[i] = stdout.String(), stderr.String()
		}()
	}
	wg.Wait()
	winner := ""
	for i, c := range codes {
		switch c {
		case cli.ExitOK:
			var res struct {
				Data struct {
					Item struct {
						Assignee string `json:"assignee"`
					} `json:"item"`
					Changed bool `json:"changed"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(stdouts[i]), &res); err != nil {
				t.Fatal(err)
			}
			if !res.Data.Changed || winner != "" {
				t.Errorf("run %d: a second successful claim: %s", i, stdouts[i])
			}
			winner = res.Data.Item.Assignee
		case cli.ExitConflict:
			if !strings.Contains(stderrs[i], `"claim_conflict"`) {
				t.Errorf("run %d: conflict without claim_conflict: %s", i, stderrs[i])
			}
		default:
			t.Errorf("run %d: exit %d: %s", i, c, stderrs[i])
		}
	}
	if winner == "" {
		t.Fatal("no run claimed the Item")
	}
	b, err := os.ReadFile(filepath.Join(vault, "Projects", "WEB", "PRDs", "WEB-1 Landing page.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("\nassignee: "+winner+"\n")) {
		t.Errorf("the file does not name the winner %s:\n%s", winner, b)
	}
}
