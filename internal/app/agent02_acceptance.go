package app

import (
	"context"
	"encoding/json"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"asimi/internal/command"
	"asimi/internal/developer"
)

type agentAcceptance struct {
	Version   int      `json:"version"`
	Kind      string   `json:"kind"`
	Paths     []string `json:"paths,omitempty"`
	CommandID string   `json:"commandId,omitempty"`
	Args      []string `json:"args,omitempty"`
}

type agentCondition struct {
	Kind     string   `json:"kind"`
	Status   string   `json:"status"`
	Evidence []string `json:"evidence,omitempty"`
}

type agentVerification struct {
	Passed     bool             `json:"passed"`
	Conditions []agentCondition `json:"conditions"`
}

var (
	agentPathMention = regexp.MustCompile("(?i)[a-z0-9_./\\\\-]+\\.(?:go|ts|tsx|js|jsx|mjs|cjs|py|java|cs|vue|json|md|html|css|rs|c|cpp|h|yaml|yml|toml)\\b")
	agentGoTestGoal  = regexp.MustCompile("(?:^|[\\s:：\x60])go test(?:[ \\t]+([^\\r\\n\x60。;；]+))?")
	agentReadGoal    = regexp.MustCompile("(?i)^(?:explain|analy[sz]e|inspect|summari[sz]e|review|read|show|describe)\\b|^(?:帮我|请)?(?:分析|解释|查看|检查|阅读|总结|梳理)")
	agentRepairGoal  = regexp.MustCompile("(?i)\\b(?:fix|repair|edit|modify|update|change|implement|add|replace)\\b|修复|修改|更改|实现|替换|新增")
	agentTestGoal    = regexp.MustCompile("(?i)\\b(?:run|execute|verify)\\b.{0,32}\\btests?\\b|(?:运行|执行|跑一下|验证).{0,16}测试")
	agentOtherGoal   = regexp.MustCompile("(?i)\\b(?:send|publish|deploy|delete|upload|download|install|open|click|browse|email|github|gitlab|notepad|push|commit|merge|remove|rename|move|create|save|launch|export)\\b|发送|发布|部署|删除|上传|下载|安装|打开|点击|浏览器|邮件|提交|合并|创建|保存|导出")
	agentGoTestPass  = regexp.MustCompile(`(?m)^ok[\t ]+(\S+)[\t ]+(?:[0-9.]+s|\(cached\))[\t \r]*$`)
	agentReadJoin    = regexp.MustCompile("(?i)\\b(?:then|after that|followed by)\\b|[;；]|然后|随后")
)

func agentAcceptanceForGoal(goal string) agentAcceptance {
	goal = strings.TrimSpace(goal)
	out := agentAcceptance{Version: 1, Kind: "clarify"}
	if agentOtherGoal.MatchString(goal) {
		return out
	}
	for _, candidate := range agentPathMention.FindAllString(goal, -1) {
		name := strings.TrimPrefix(strings.ReplaceAll(candidate, "\\", "/"), "./")
		if developer.AgentContextPathAllowed(name) && !slices.Contains(out.Paths, name) {
			out.Paths = append(out.Paths, name)
		}
	}
	switch {
	case agentRepairGoal.MatchString(goal) && (!agentReadGoal.MatchString(goal) || agentReadJoin.MatchString(goal) ||
		strings.Contains(strings.ToLower(goal), " and ") || strings.ContainsAny(goal, "并再")):
		out.Kind = "repair"
	case agentGoTestGoal.MatchString(goal), agentTestGoal.MatchString(goal):
		out.Kind = "test"
	case agentReadGoal.MatchString(goal) && !agentReadJoin.MatchString(goal):
		out.Kind = "read"
	}
	if found := agentGoTestGoal.FindStringSubmatch(goal); len(found) > 0 && out.Kind != "read" {
		out.CommandID = "go.test"
		out.Args = strings.Fields(found[1])
		if len(out.Args) == 0 {
			out.Args = []string{"."}
		}
		for _, arg := range out.Args {
			if !strings.HasPrefix(arg, "./") && arg != "." {
				out.Kind = "clarify"
				break
			}
		}
	}
	return out
}

func agentTestCommand(id string) bool {
	switch id {
	case "go.test", "node.test", "java.test", "dotnet.test", "python.pytest", "python.unittest":
		return true
	default:
		return false
	}
}

func agentMatchingTest(rule *agentAcceptance, cmd *command.Proposal, workspace string) bool {
	if cmd == nil || cmd.DirectoryID != workspace || !agentTestCommand(cmd.ID) {
		return false
	}
	if rule.CommandID != "" && cmd.ID != rule.CommandID {
		return false
	}
	return rule.CommandID != "go.test" || slices.Equal(cmd.Args, append([]string{"test"}, rule.Args...))
}

// agentGoPackage resolves a file against the nearest readable go.mod.
func agentGoPackage(ctx context.Context, root, file string, options developer.Options) string {
	folder := path.Dir(file)
	for dir := folder; ; dir = path.Dir(dir) {
		manifest, err := developer.ReadContext(ctx, root, path.Join(dir, "go.mod"), options)
		if err == nil {
			for line := range strings.Lines(manifest.Content) {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "module" {
					relative := strings.TrimPrefix(folder, dir+"/")
					if dir == "." {
						relative = folder
					}
					if folder == dir {
						relative = "."
					}
					return path.Join(strings.Trim(fields[1], "\""), relative)
				}
			}
			return ""
		}
		if dir == "." || errorCode(err) != "FILE_NOT_FOUND" {
			return ""
		}
	}
}

// A root-only Go test does not cover a change in a different package.
func agentTestCoversFile(cmd *command.Proposal, file string) bool {
	if cmd.ID != "go.test" {
		// An arbitrary narrowed test in another runner can't be assumed to
		// cover this file. Only the registered default invocation qualifies.
		for _, spec := range command.Catalog() {
			if spec.ID == cmd.ID {
				return slices.Equal(spec.Args, cmd.Args)
			}
		}
		return false
	}
	folder := path.Dir(file)
	if len(cmd.Args) == 1 {
		return folder == "."
	}
	for _, arg := range cmd.Args[1:] {
		arg = strings.ReplaceAll(arg, "\\", "/")
		if arg == "./..." || arg == "..." {
			return true
		}
		if prefix, recursive := strings.CutSuffix(arg, "/..."); recursive {
			prefix = path.Clean(prefix)
			if folder == prefix || strings.HasPrefix(folder, prefix+"/") {
				return true
			}
		} else if path.Clean(arg) == folder {
			return true
		}
	}
	return false
}

func (s *Service) verifyAgent02(ctx context.Context, p agent02Preview, current []AgentStep) (agentVerification, error) {
	result := agentVerification{}
	rule := p.task.Agent.Acceptance
	if rule == nil || rule.Version != 1 || rule.Kind == "clarify" {
		result.Conditions = []agentCondition{{Kind: "clarify", Status: "UNKNOWN"}}
		return result, nil
	}
	if rule.Kind == "read" {
		read := agentCondition{Kind: "read", Status: "MISSING"}
		seen := map[string]bool{}
		for _, file := range p.project.Files {
			if validAgentSHA(file.SHA256) {
				seen[file.Path] = true
				read.Evidence = append(read.Evidence, file.Path)
			}
		}
		for _, step := range current {
			if step.Status != "COMPLETED" || len(step.Result) == 0 {
				continue
			}
			switch step.Kind {
			case "agent.read":
				var file struct{ Path, SHA256 string }
				if json.Unmarshal(step.Result, &file) == nil && validAgentSHA(file.SHA256) {
					seen[file.Path] = true
					read.Evidence = append(read.Evidence, step.ID)
				}
			case "agent.analysis", "agent.workspace":
				read.Evidence = append(read.Evidence, step.ID)
			}
		}
		result.Passed = len(read.Evidence) > 0
		for _, file := range rule.Paths {
			result.Passed = result.Passed && seen[file]
		}
		if result.Passed {
			read.Status = "SATISFIED"
		}
		result.Conditions = []agentCondition{read}
		// An unresolved effect remains material even for a read-only goal.
		for _, step := range p.task.Steps {
			if step.ActionID != "" && (step.Status == "RECOVERY_REQUIRED" || step.Status == "EXECUTING") {
				result.Passed = false
				result.Conditions = append(result.Conditions, agentCondition{Kind: "recovery", Status: "UNKNOWN", Evidence: []string{step.ActionID}})
			}
		}
		return result, nil
	}
	if rule.Kind != "repair" && rule.Kind != "test" {
		result.Conditions = []agentCondition{{Kind: "clarify", Status: "UNKNOWN"}}
		return result, nil
	}

	file := agentCondition{Kind: "file", Status: "MISSING"}
	check := agentCondition{Kind: "test", Status: "MISSING"}
	type editWitness struct {
		action ProductAction
		result developer.ChangeResult
	}
	edits := map[string]editWitness{}
	var lastChange int
	var lastTest *ProductAction
	var passingGoPackages map[string]bool
	var unresolved bool
	for index, step := range p.task.Steps {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if step.ActionID == "" {
			continue
		}
		a, err := loadProductAction(s.db, step.ActionID)
		if errorCode(err) == "NOT_FOUND" {
			continue
		}
		if err != nil {
			return result, err
		}
		proposal := a.Proposal
		version := proposal.PlanVersion
		if a.TaskID != p.task.ID || proposal.WorkspaceID != p.workspace.ID || proposal.Root != p.workspace.Root ||
			proposal.StepID != step.ID || a.Hash != productHash(a) ||
			version < 1 || version > len(p.task.Agent.Rounds) || p.task.Agent.Rounds[version-1].ActionID != a.ID {
			continue
		}
		if a.Status == "EXECUTING" || a.Status == "RECOVERY_REQUIRED" {
			unresolved = true
		}
		// A subsequent attempted test can supersede an earlier pass. Proposals
		// rejected or expired before consumption carry no test result.
		if a.Kind == "command" && proposal.Command != nil && proposal.Command.DirectoryID == p.task.Agent.BoundWorkspace &&
			agentTestCommand(proposal.Command.ID) && (lastTest == nil || lastTest.Proposal.Command.ID == proposal.Command.ID) &&
			(a.Status == "FAILED" || (a.Status == "CANCELLED" && len(a.Result) > 0 && string(a.Result) != "null")) {
			lastTest = nil
		}
		if a.Status != "SUCCEEDED" || step.Status != "COMPLETED" {
			continue
		}
		var durable bool
		err = s.db.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM product_actions a JOIN product_action_intents i ON i.action_id=a.id
			WHERE a.id=? AND a.consumed=1 AND i.settled=1 AND i.hash=a.hash AND i.proposal_json=a.proposal_json
		)`, a.ID).Scan(&durable)
		if err != nil {
			return result, err
		}
		if !durable {
			continue
		}
		switch a.Kind {
		case "file":
			if proposal.File == nil || proposal.File.RootIdentity != p.task.Agent.BoundWorkspace {
				continue
			}
			var witness developer.ChangeResult
			if json.Unmarshal(a.Result, &witness) == nil && witness.Applied && witness.Verified &&
				witness.Identity != "" && witness.RelativePath == proposal.File.RelativePath &&
				witness.AfterHash == proposal.File.AfterHash && proposal.File.BeforeHash != proposal.File.AfterHash {
				edits[witness.RelativePath] = editWitness{a, witness}
				lastChange = index + 1
				lastTest = nil
			}
		case "command":
			if !agentMatchingTest(rule, proposal.Command, p.task.Agent.BoundWorkspace) {
				continue
			}
			var witness struct {
				ExitCode  *int   `json:"exitCode"`
				Cancelled bool   `json:"cancelled"`
				TimedOut  bool   `json:"timedOut"`
				Stdout    string `json:"stdout"`
			}
			if index >= lastChange && json.Unmarshal(a.Result, &witness) == nil &&
				witness.ExitCode != nil && *witness.ExitCode == 0 && !witness.Cancelled && !witness.TimedOut &&
				(proposal.Command.ID != "go.test" || agentGoTestPass.MatchString(witness.Stdout)) {
				lastTest = &a
				passingGoPackages = map[string]bool{}
				for _, match := range agentGoTestPass.FindAllStringSubmatch(witness.Stdout, -1) {
					passingGoPackages[match[1]] = true
				}
			} else {
				lastTest = nil
			}
		}
	}

	if rule.Kind == "repair" {
		passed := len(edits) != 0
		for _, target := range rule.Paths {
			if _, ok := edits[target]; !ok {
				passed = false
			}
		}
		for _, name := range slices.Sorted(maps.Keys(edits)) {
			witness := edits[name]
			if err := ctx.Err(); err != nil {
				return result, err
			}
			after, err := developer.ReadContext(ctx, p.workspace.Root, name, developer.Options{Exclusions: slices.Clone(p.task.Agent.Exclusions)})
			if err != nil || after.Identity != witness.result.Identity || after.Hash != witness.result.AfterHash {
				passed = false
				continue
			}
			file.Evidence = append(file.Evidence, witness.action.ID)
		}
		if passed {
			file.Status = "SATISFIED"
		} else {
			lastTest = nil
		}
		result.Conditions = append(result.Conditions, file)
	}

	if lastTest != nil {
		check.Status = "SATISFIED"
		targets := slices.Sorted(maps.Keys(edits))
		if rule.Kind == "test" {
			targets = rule.Paths
		}
		for _, name := range targets {
			cmd := lastTest.Proposal.Command
			covered := agentTestCoversFile(cmd, name)
			if cmd.ID == "go.test" {
				pkg := agentGoPackage(ctx, p.workspace.Root, name, developer.Options{Exclusions: slices.Clone(p.task.Agent.Exclusions)})
				covered = strings.EqualFold(path.Ext(name), ".go") && covered && passingGoPackages[pkg]
			} else if rule.Kind == "test" {
				covered = slices.Contains(cmd.Args, "./"+name)
			}
			if !covered {
				check.Status = "MISSING"
				break
			}
		}
		if check.Status == "SATISFIED" {
			check.Evidence = []string{lastTest.ID}
		}
	}
	result.Conditions = append(result.Conditions, check)
	if unresolved {
		result.Conditions = append(result.Conditions, agentCondition{Kind: "recovery", Status: "UNKNOWN"})
	}
	result.Passed = !unresolved && check.Status == "SATISFIED" && (rule.Kind != "repair" || file.Status == "SATISFIED")
	return result, nil
}
