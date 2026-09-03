package tools

import (
	"path"
	"strings"
	"unicode"
)

// ownerOnlyTools are agent-management / provisioning tools. Guests must
// not see them in the LLM catalog and must not be able to invoke them
// even if the model hallucinates the name.
var ownerOnlyTools = map[string]bool{
	"create_agent":    true,
	"configure_agent": true,
	"check_agent":     true,
	"install_skill":   true,
	"search_skills":   true,
	"ssh_exec":        true,
}

// hostOnlyTools require platform host access (super_admin). Distinct
// from ownerOnlyTools: an agent owner who is not a super_admin still
// must not see host_exec.
var hostOnlyTools = map[string]bool{
	HostExecToolName: true,
}

// protectedConfigBasenames are files a guest must not read via exec
// (cat /skills/foo/SKILL.md, cat SOUL.md, …). Identity-file tools
// already gate the same set; this is the shell bypass.
var protectedConfigBasenames = []string{
	"soul.md",
	"identity.md",
	"bootstrap.md",
	"agents.md",
	"tools.md",
	"heartbeat.md",
	"agent.json",
	"knowledge.md",
	"skill.md",
}

const errGuestConfigExec = "reading agent configuration (persona files, SKILL.md, agent.json) via the shell is not allowed — the command was NOT run"

const errToolHiddenFromCaller = "this tool is not available to the current chatter"

// toolVisibleToCaller reports whether the LLM catalog and invoke path
// should expose `name` on this turn.
func (r *Registry) toolVisibleToCaller(name string) bool {
	if r == nil {
		return false
	}
	if ownerOnlyTools[name] && !r.callerIsAdmin {
		return false
	}
	if hostOnlyTools[name] && !r.callerCanHost {
		return false
	}
	return true
}

// DenyIfHidden is the invoke-time gate used by Execute and the SDK
// adapter so a hallucinated create_agent / host_exec call still fails
// after the catalog hid the tool.
func (r *Registry) DenyIfHidden(name string) error {
	if r.toolVisibleToCaller(name) {
		return nil
	}
	return errHiddenTool
}

type hiddenToolError struct{}

func (hiddenToolError) Error() string { return errToolHiddenFromCaller }

var errHiddenTool hiddenToolError

// guestExecReadsConfig reports whether a shell command (or its stdin
// script) looks like it is trying to read a protected config file.
// Globs are collapsed so `cat /ski*/x/SKI*.md` still matches.
//
// Applied only for non-admin chatters; owners may inspect their own
// files from the host or sandbox.
func guestExecReadsConfig(command, stdin string) bool {
	return mentionsProtectedConfig(command) || mentionsProtectedConfig(stdin)
}

func mentionsProtectedConfig(s string) bool {
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	collapsed := stripGlobMeta(lower)
	for _, name := range protectedConfigBasenames {
		if pathMentionsBasename(lower, name) || pathMentionsBasename(collapsed, name) {
			return true
		}
	}
	for _, tok := range commandPathTokens(lower) {
		base := tok
		if i := strings.LastIndexAny(tok, "/\\"); i >= 0 {
			base = tok[i+1:]
		}
		if base == "" || !strings.ContainsAny(base, "*?") {
			continue
		}
		for _, name := range protectedConfigBasenames {
			if ok, _ := path.Match(base, name); ok {
				return true
			}
		}
	}
	return false
}

func commandPathTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '"', '\'', '`', ';', '|', '&', '<', '>', '(', ')':
			return true
		default:
			return false
		}
	})
}

func stripGlobMeta(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '*', '?', '[', ']':
			return -1
		default:
			return r
		}
	}, s)
}

// pathMentionsBasename is true when `name` appears as a path segment
// (bounded by non-filename characters). "report-about-soul.md" does
// not match "soul.md"; "cat SOUL.md" and "/skills/foo/SKILL.md" do.
func pathMentionsBasename(hay, name string) bool {
	if name == "" {
		return false
	}
	from := 0
	for {
		i := strings.Index(hay[from:], name)
		if i < 0 {
			return false
		}
		i += from
		beforeOK := i == 0 || !isFilenameChar(rune(hay[i-1]))
		after := i + len(name)
		afterOK := after >= len(hay) || !isFilenameChar(rune(hay[after]))
		if beforeOK && afterOK {
			return true
		}
		from = i + 1
	}
}

func isFilenameChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.'
}

// filterGuestDirListing drops identity files and SKILL.md from a
// list_dir listing so a guest cannot inventory the agent's private
// config. Workspace artifacts and skill *directories* stay visible
// (folder names already appear in the skill catalog).
func (r *Registry) filterGuestDirListing(listing string) string {
	if r == nil || r.callerIsAdmin || listing == "" {
		return listing
	}
	var keep []string
	for _, line := range strings.Split(listing, "\n") {
		if line == "" {
			keep = append(keep, line)
			continue
		}
		name := dirListingName(line)
		if guestHiddenDirEntry(name) {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// dirListingName extracts the filename from "f NAME (N bytes)" /
// "d NAME/" lines produced by makeListDir.
func dirListingName(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	return strings.TrimSuffix(fields[1], "/")
}

func guestHiddenDirEntry(name string) bool {
	if name == "" {
		return false
	}
	if identityFiles[name] || ownerScopedSystemFiles[name] {
		return true
	}
	return strings.EqualFold(name, "SKILL.md")
}
