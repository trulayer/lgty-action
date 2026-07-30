package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

const (
	codecovAction          = "codecov/codecov-action@0fb7174895f61a3b6b78fc075e0cd60383518dac"
	uploadArtifactAction   = "actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02"
	downloadArtifactAction = "actions/download-artifact@d3f86a106a0bac45b974a628896c90dbdf5c8093"
	badgeURL               = "https://codecov.io/gh/trulayer/lgty-action/graph/badge.svg"
)

type workflowJob struct {
	permissions     map[string]string
	ifCondition     string
	continueOnError string
	needs           string
	steps           []workflowStep
}

type workflowStep struct {
	name            string
	uses            string
	run             string
	ifCondition     string
	continueOnError string
	env             map[string]string
	with            map[string]string
}

type workflowContract struct {
	defaultPermissions map[string]string
	jobs               map[string]workflowJob
}

func TestCodecovUploadContract(t *testing.T) {
	workflow := readContractFile(t, ".github/workflows/ci.yml")
	if err := validateCodecovWorkflow(workflow); err != nil {
		t.Fatal(err)
	}
}

func TestCodecovUploadContractRejectsAdversarialChanges(t *testing.T) {
	workflow := readContractFile(t, ".github/workflows/ci.yml")
	tests := map[string]string{
		"commented OIDC flag": strings.Replace(
			workflow, "          use_oidc: true", "          # use_oidc: true", 1,
		),
		"moved upload step": strings.Replace(
			workflow, "  coverage:\n", "  coverage-moved:\n", 1,
		),
		"missing step continue-on-error": strings.Replace(
			workflow, "        continue-on-error: true\n        with:\n          use_oidc: true",
			"        with:\n          use_oidc: true", 1,
		),
		"gating Codecov flag": strings.Replace(
			workflow, "          use_oidc: true", "          use_oidc: true\n          fail_ci_if_error: true", 1,
		),
		"permission leaked to build": strings.Replace(
			workflow, "  build:\n    permissions:\n      contents: read",
			"  build:\n    permissions:\n      contents: read\n      id-token: write", 1,
		),
		"inline permission leaked to build": strings.Replace(
			workflow, "  build:\n    permissions:\n      contents: read",
			"  build:\n    permissions: {contents: read, id-token: write}", 1,
		),
		"quoted inline permission leaked to build": strings.Replace(
			workflow, "  build:\n    permissions:\n      contents: read",
			`  build:
    permissions: {"contents": read, "id-token": write}`, 1,
		),
		"permission leaked to semgrep": strings.Replace(
			workflow, "  semgrep:\n    runs-on:", "  semgrep:\n    permissions:\n      id-token: write\n    runs-on:", 1,
		),
		"permission leaked to aggregate": strings.Replace(
			workflow, "    permissions: {}\n    steps:", "    permissions:\n      id-token: write\n    steps:", 1,
		),
		"govulncheck moved to coverage": strings.Replace(
			strings.Replace(
				workflow,
				"      - name: govulncheck\n        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...\n\n",
				"", 1,
			),
			"      - name: Upload coverage reports to Codecov",
			"      - name: govulncheck\n        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...\n\n      - name: Upload coverage reports to Codecov",
			1,
		),
		"govulncheck continue-on-error": strings.Replace(
			workflow,
			"        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...",
			"        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...\n        continue-on-error: true",
			1,
		),
		"govulncheck quoted continue-on-error": strings.Replace(
			workflow,
			"        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...",
			"        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...\n        continue-on-error: \"true\"",
			1,
		),
		"govulncheck skipped": strings.Replace(
			workflow,
			"        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...",
			"        if: false\n        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...",
			1,
		),
		"commented govulncheck command": strings.Replace(
			workflow,
			"        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...",
			"        # run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...",
			1,
		),
		"flow-style tolerated govulncheck": strings.Replace(
			workflow,
			"      - name: govulncheck\n        run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...",
			"      - {name: govulncheck, run: go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./..., continue-on-error: true}",
			1,
		),
		"aggregate continue-on-error": strings.Replace(
			workflow, "  ci:\n    if: always()", "  ci:\n    if: always()\n    continue-on-error: true", 1,
		),
		"aggregate quoted continue-on-error": strings.Replace(
			workflow, "  ci:\n    if: always()", "  ci:\n    if: always()\n    continue-on-error: \"true\"", 1,
		),
		"aggregate false condition": strings.Replace(
			workflow, "  ci:\n    if: always()", "  ci:\n    if: false", 1,
		),
		"aggregate unsafe always condition": strings.Replace(
			workflow, "  ci:\n    if: always()", "  ci:\n    if: always() || true", 1,
		),
		"aggregate expression always variant": strings.Replace(
			workflow, "  ci:\n    if: always()", "  ci:\n    if: ${{ always() || true }}", 1,
		),
		"aggregate commented safe flag": strings.Replace(
			workflow, "  ci:\n    if: always()",
			"  ci:\n    if: always()\n    continue-on-error: true\n    # continue-on-error: false", 1,
		),
		"aggregate flow mapping": strings.Replace(
			workflow, "  ci:\n    if: always()\n    needs: [build, semgrep]",
			"  ci: {if: always(), continue-on-error: true, needs: [build, semgrep]}", 1,
		),
		"aggregate run true": strings.Replace(
			workflow,
			"          test \"$BUILD_RESULT\" = success\n          test \"$SEMGREP_RESULT\" = success",
			"          true", 1,
		),
		"aggregate missing result assertion": strings.Replace(
			workflow, "          test \"$SEMGREP_RESULT\" = success\n", "", 1,
		),
		"aggregate renamed result assertion": strings.Replace(
			workflow, "test \"$BUILD_RESULT\" = success", "test \"$BUILD_STATUS\" = success", 1,
		),
		"aggregate step continue-on-error": strings.Replace(
			workflow,
			"      - name: Require every blocking CI job\n        env:",
			"      - name: Require every blocking CI job\n        continue-on-error: true\n        env:", 1,
		),
		"aggregate step condition": strings.Replace(
			workflow,
			"      - name: Require every blocking CI job\n        env:",
			"      - name: Require every blocking CI job\n        if: false\n        env:", 1,
		),
		"aggregate misleading comment": strings.Replace(
			workflow,
			"          test \"$BUILD_RESULT\" = success",
			"          # test \"$BUILD_RESULT\" = success\n          true", 1,
		),
		"aggregate env indirection": strings.Replace(
			workflow,
			"          BUILD_RESULT: ${{ needs.build.result }}",
			"          BUILD_STATUS: ${{ needs.build.result }}", 1,
		),
	}

	for name, mutated := range tests {
		t.Run(name, func(t *testing.T) {
			if mutated == workflow {
				t.Fatal("test mutation did not change workflow")
			}
			if err := validateCodecovWorkflow(mutated); err == nil {
				t.Fatal("unsafe workflow unexpectedly passed validation")
			}
		})
	}
}

func TestCodecovBadgeDoesNotCarryToken(t *testing.T) {
	readme := readContractFile(t, "README.md")
	if !strings.Contains(readme, badgeURL+")") {
		t.Fatal("README must retain the plain public Codecov badge")
	}
	if strings.Contains(readme, badgeURL+"?") {
		t.Fatal("public Codecov badge must not contain query credentials")
	}
}

func validateCodecovWorkflow(source string) error {
	contract, uncommented, err := parseWorkflowContract(source)
	if err != nil {
		return err
	}
	if strings.Contains(uncommented, "CODECOV_TOKEN") {
		return fmt.Errorf("workflow must not reference CODECOV_TOKEN")
	}
	if got := formatMap(contract.defaultPermissions); got != "contents=read" {
		return fmt.Errorf("default permissions must be contents=read, got %s", got)
	}

	coverage, ok := contract.jobs["coverage"]
	if !ok {
		return fmt.Errorf("dedicated coverage job not found")
	}
	if coverage.needs != "build" {
		return fmt.Errorf("coverage job must consume output from build")
	}
	if coverage.continueOnError != "true" {
		return fmt.Errorf("coverage job must be informational")
	}
	if got := formatMap(coverage.permissions); got != "contents=read,id-token=write" {
		return fmt.Errorf("coverage permissions exceed required minimum: %s", got)
	}
	if len(coverage.steps) != 2 {
		return fmt.Errorf("coverage job may only download and publish the inert artifact")
	}

	var codecovSteps, uploadSteps, downloadSteps []workflowStep
	govulnJob := ""
	var govulnStep workflowStep
	for jobName, job := range contract.jobs {
		if jobName != "coverage" {
			if _, hasOIDC := job.permissions["id-token"]; hasOIDC {
				return fmt.Errorf("%s job must not receive id-token permission", jobName)
			}
		}
		for _, step := range job.steps {
			if step.run == "go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./..." {
				if govulnJob != "" {
					return fmt.Errorf("govulncheck must run exactly once")
				}
				govulnJob = jobName
				govulnStep = step
			}
			switch step.uses {
			case uploadArtifactAction:
				if jobName != "build" {
					return fmt.Errorf("coverage artifact must be produced by build")
				}
				uploadSteps = append(uploadSteps, step)
			case downloadArtifactAction:
				if jobName != "coverage" {
					return fmt.Errorf("coverage artifact must be consumed by coverage job")
				}
				downloadSteps = append(downloadSteps, step)
			}
			if strings.HasPrefix(step.uses, "codecov/codecov-action@") {
				if jobName != "coverage" {
					return fmt.Errorf("Codecov step must remain in dedicated coverage job")
				}
				codecovSteps = append(codecovSteps, step)
			}
		}
	}
	if govulnJob == "" {
		return fmt.Errorf("blocking govulncheck step not found")
	}
	if govulnJob == "coverage" {
		return fmt.Errorf("govulncheck must not run in informational OIDC job")
	}
	if govulnStep.ifCondition != "" {
		return fmt.Errorf("govulncheck step must execute unconditionally")
	}
	if govulnStep.continueOnError != "" {
		return fmt.Errorf("govulncheck step must not continue on error")
	}
	if contract.jobs[govulnJob].continueOnError != "" &&
		contract.jobs[govulnJob].continueOnError != "false" {
		return fmt.Errorf("govulncheck job must remain blocking")
	}
	aggregate, ok := contract.jobs["ci"]
	if !ok || !inlineListContains(aggregate.needs, govulnJob) {
		return fmt.Errorf("aggregate ci must gate on govulncheck job %q", govulnJob)
	}
	if got := formatInlineList(aggregate.needs); got != "build,semgrep" {
		return fmt.Errorf("aggregate ci needs must retain exact blocking jobs, got %s", got)
	}
	if aggregate.ifCondition != "always()" {
		return fmt.Errorf("aggregate ci job must use exact fail-closed if: always()")
	}
	if aggregate.continueOnError != "" {
		return fmt.Errorf("aggregate ci job must not continue on error")
	}
	if len(aggregate.steps) != 1 {
		return fmt.Errorf("aggregate ci must contain exactly one assertion step")
	}
	aggregateStep := aggregate.steps[0]
	if aggregateStep.name != "Require every blocking CI job" ||
		aggregateStep.ifCondition != "" ||
		aggregateStep.continueOnError != "" ||
		aggregateStep.uses != "" {
		return fmt.Errorf("aggregate ci assertion step must remain unconditional and blocking")
	}
	for _, dependency := range []string{"build", "semgrep"} {
		envName := strings.ToUpper(dependency) + "_RESULT"
		wantEnv := "${{ needs." + dependency + ".result }}"
		if aggregateStep.env[envName] != wantEnv {
			return fmt.Errorf("aggregate ci must bind %s to %s", envName, wantEnv)
		}
	}
	if len(aggregateStep.env) != 2 {
		return fmt.Errorf("aggregate ci assertion environment must contain only dependency results")
	}
	const wantAggregateRun = "test \"$BUILD_RESULT\" = success\ntest \"$SEMGREP_RESULT\" = success"
	if aggregateStep.run != wantAggregateRun {
		return fmt.Errorf("aggregate ci must assert every exact dependency result")
	}
	if len(uploadSteps) != 1 ||
		uploadSteps[0].continueOnError != "true" ||
		uploadSteps[0].with["name"] != "coverage-report" ||
		uploadSteps[0].with["path"] != "coverage.out" ||
		uploadSteps[0].with["if-no-files-found"] != "error" ||
		uploadSteps[0].with["retention-days"] != "1" {
		return fmt.Errorf("build must produce exactly one short-lived inert coverage artifact")
	}
	if len(downloadSteps) != 1 || downloadSteps[0].with["name"] != "coverage-report" {
		return fmt.Errorf("coverage job must consume exactly one coverage-report artifact")
	}
	if len(codecovSteps) != 1 {
		return fmt.Errorf("expected exactly one Codecov step, got %d", len(codecovSteps))
	}
	step := codecovSteps[0]
	if step.uses != codecovAction {
		return fmt.Errorf("Codecov action must use audited SHA, got %q", step.uses)
	}
	if step.continueOnError != "true" {
		return fmt.Errorf("Codecov step must continue on error")
	}
	if step.with["use_oidc"] != "true" {
		return fmt.Errorf("Codecov step must set use_oidc: true")
	}
	if step.with["files"] != "./coverage.out" {
		return fmt.Errorf("Codecov step must upload only the inert coverage artifact")
	}
	if _, present := step.with["token"]; present {
		return fmt.Errorf("Codecov step must not contain token input")
	}
	if value, present := step.with["fail_ci_if_error"]; present && value != "false" {
		return fmt.Errorf("fail_ci_if_error must be absent or false")
	}
	return nil
}

// parseWorkflowContract parses the workflow structure needed by the security
// contract. It strips YAML comments outside quotes, then recognizes mappings
// only at their exact schema indentation, so commented or relocated keys
// cannot satisfy the checks.
func parseWorkflowContract(source string) (workflowContract, string, error) {
	contract := workflowContract{
		defaultPermissions: map[string]string{},
		jobs:               map[string]workflowJob{},
	}
	lines := strings.Split(source, "\n")
	cleaned := make([]string, len(lines))
	for i, line := range lines {
		cleaned[i] = stripYAMLComment(line)
	}

	inJobs := false
	currentJob := ""
	currentStep := -1
	section := ""
	for lineNumber, line := range cleaned {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		text := strings.TrimSpace(line)

		switch {
		case indent == 0 && text == "jobs:":
			inJobs = true
			currentJob, section = "", ""
		case indent == 0 && text == "permissions:" && !inJobs:
			section = "default-permissions"
		case !inJobs && section == "default-permissions" && indent == 2:
			key, value, ok := splitYAMLField(text)
			if !ok {
				return contract, strings.Join(cleaned, "\n"), fmt.Errorf("invalid default permission at line %d", lineNumber+1)
			}
			contract.defaultPermissions[key] = value
		case inJobs && indent == 2 && strings.HasSuffix(text, ":"):
			currentJob = strings.TrimSuffix(text, ":")
			if _, duplicate := contract.jobs[currentJob]; duplicate {
				return contract, strings.Join(cleaned, "\n"), fmt.Errorf("duplicate job %q", currentJob)
			}
			contract.jobs[currentJob] = workflowJob{permissions: map[string]string{}}
			currentStep, section = -1, ""
		case currentJob != "" && indent == 4:
			job := contract.jobs[currentJob]
			key, value, ok := splitYAMLField(text)
			if !ok {
				continue
			}
			switch key {
			case "permissions":
				if strings.HasPrefix(value, "{") {
					parsed, err := parseFlowMap(value)
					if err != nil {
						return contract, strings.Join(cleaned, "\n"), fmt.Errorf("invalid permissions at line %d: %w", lineNumber+1, err)
					}
					job.permissions = parsed
					section = ""
				} else if value != "" {
					return contract, strings.Join(cleaned, "\n"), fmt.Errorf("unsupported permissions syntax at line %d", lineNumber+1)
				} else {
					section = "job-permissions"
				}
			case "steps":
				section = "steps"
			case "continue-on-error":
				job.continueOnError = value
			case "if":
				job.ifCondition = value
			case "needs":
				job.needs = value
			default:
				section = ""
			}
			contract.jobs[currentJob] = job
		case currentJob != "" && section == "job-permissions" && indent == 6:
			key, value, ok := splitYAMLField(text)
			if ok {
				job := contract.jobs[currentJob]
				job.permissions[key] = value
				contract.jobs[currentJob] = job
			}
		case currentJob != "" && strings.HasPrefix(section, "step") && indent == 6 && strings.HasPrefix(text, "- "):
			step := workflowStep{env: map[string]string{}, with: map[string]string{}}
			job := contract.jobs[currentJob]
			job.steps = append(job.steps, step)
			currentStep = len(job.steps) - 1
			section = "steps"
			contract.jobs[currentJob] = job
			if key, value, ok := splitYAMLField(strings.TrimPrefix(text, "- ")); ok {
				switch key {
				case "name":
					job.steps[currentStep].name = value
				case "uses":
					job.steps[currentStep].uses = value
				}
				contract.jobs[currentJob] = job
			}
		case currentJob != "" && strings.HasPrefix(section, "step") && currentStep >= 0 && indent == 8:
			key, value, ok := splitYAMLField(text)
			if !ok {
				continue
			}
			job := contract.jobs[currentJob]
			switch key {
			case "name":
				job.steps[currentStep].name = value
			case "uses":
				job.steps[currentStep].uses = value
			case "run":
				if value == "|" || value == ">" || value == "|-" || value == ">-" {
					job.steps[currentStep].run = ""
					section = "step-run"
				} else {
					job.steps[currentStep].run = value
				}
			case "if":
				job.steps[currentStep].ifCondition = value
			case "continue-on-error":
				job.steps[currentStep].continueOnError = value
			case "env":
				section = "step-env"
			case "with":
				section = "step-with"
			}
			contract.jobs[currentJob] = job
		case currentJob != "" && section == "step-with" && currentStep >= 0 && indent == 10:
			key, value, ok := splitYAMLField(text)
			if ok {
				job := contract.jobs[currentJob]
				job.steps[currentStep].with[key] = value
				contract.jobs[currentJob] = job
			}
		case currentJob != "" && section == "step-env" && currentStep >= 0 && indent == 10:
			key, value, ok := splitYAMLField(text)
			if ok {
				job := contract.jobs[currentJob]
				job.steps[currentStep].env[key] = value
				contract.jobs[currentJob] = job
			}
		case currentJob != "" && section == "step-run" && currentStep >= 0 && indent >= 10:
			job := contract.jobs[currentJob]
			if job.steps[currentStep].run != "" {
				job.steps[currentStep].run += "\n"
			}
			job.steps[currentStep].run += text
			contract.jobs[currentJob] = job
		}
	}
	return contract, strings.Join(cleaned, "\n"), nil
}

func stripYAMLComment(line string) string {
	var quote rune
	for i, char := range line {
		switch {
		case quote == 0 && (char == '\'' || char == '"'):
			quote = char
		case quote == char:
			quote = 0
		case quote == 0 && char == '#' && (i == 0 || line[i-1] == ' '):
			return strings.TrimRight(line[:i], " ")
		}
	}
	return strings.TrimRight(line, " ")
}

func splitYAMLField(text string) (string, string, bool) {
	key, value, ok := strings.Cut(text, ":")
	if !ok {
		return "", "", false
	}
	return strings.Trim(strings.TrimSpace(key), `"'`), strings.Trim(strings.TrimSpace(value), `"'`), true
}

func formatMap(values map[string]string) string {
	pairs := make([]string, 0, len(values))
	for key, value := range values {
		pairs = append(pairs, key+"="+value)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

func inlineListContains(value, target string) bool {
	value = strings.TrimSpace(strings.Trim(value, "[]"))
	for item := range strings.SplitSeq(value, ",") {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

func formatInlineList(value string) string {
	value = strings.TrimSpace(strings.Trim(value, "[]"))
	items := []string{}
	for item := range strings.SplitSeq(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	sort.Strings(items)
	return strings.Join(items, ",")
}

func parseFlowMap(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return nil, fmt.Errorf("not a flow mapping")
	}
	result := map[string]string{}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "{"), "}"))
	if body == "" {
		return result, nil
	}
	for field := range strings.SplitSeq(body, ",") {
		key, scalar, ok := splitYAMLField(strings.TrimSpace(field))
		if !ok || key == "" || scalar == "" {
			return nil, fmt.Errorf("invalid flow-mapping field %q", field)
		}
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("duplicate flow-mapping key %q", key)
		}
		result[key] = scalar
	}
	return result, nil
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
