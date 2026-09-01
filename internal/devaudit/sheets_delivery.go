//go:build dev_audit

package devaudit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codagent/agent-runner/internal/stateio"
)

const connectionFileName = "development-audit-connection.json"

var stepValueHeader = []string{
	"schema_version", "observation_id", "observed_at_utc", "project", "workflow", "source_run_id", "execution_session_id", "audit_run_id", "trigger", "source_outcome", "step_id", "step_outcome", "lineage", "duration_ms", "cost_usd", "total_tokens", "source_models", "git_attribution", "commit_shas", "files_changed", "lines_added", "lines_deleted", "overall_value", "change_effect", "unique_contribution", "downstream_evidence", "confidence", "evidence_coverage", "judge_model", "rubric_version", "note",
}

// SetupInput names operator-provided source files. Paths are used only while
// importing and are deliberately never stored in Connection.
type SetupInput struct {
	ClientPath    string
	TokenPath     string
	SpreadsheetID string
	Tab           string
}

// Connection is the private, refresh-capable subset of an installed OAuth
// client and authorized-user token. It intentionally excludes access tokens,
// source-file locations, redirect URIs, and unrelated client metadata.
type Connection struct {
	SpreadsheetID string `json:"spreadsheet_id"`
	Tab           string `json:"tab"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	TokenURI      string `json:"token_uri"`
	RefreshToken  string `json:"refresh_token"`
}

// ConnectionStore owns user-scoped integration state, not Runner config.
// Home is injectable solely for isolated tests; an empty Home uses the user's
// real home directory.
type ConnectionStore struct {
	Home                  string
	allowInsecureTokenURI bool // test-only HTTP transport injection
}

func (s ConnectionStore) root() (string, error) {
	if s.Home != "" {
		return filepath.Join(s.Home, ".agent-runner"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agent-runner"), nil
}

func (s ConnectionStore) path() (string, error) {
	root, err := s.root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, connectionFileName), nil
}

func (s ConnectionStore) Import(input SetupInput) error {
	clientData, err := os.ReadFile(input.ClientPath) // #nosec G304 -- explicit CLI input is read only during one-time import.
	if err != nil {
		return fmt.Errorf("read OAuth client: %w", err)
	}
	tokenData, err := os.ReadFile(input.TokenPath) // #nosec G304 -- explicit CLI input is read only during one-time import.
	if err != nil {
		return fmt.Errorf("read OAuth token: %w", err)
	}
	var client struct {
		Installed struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			TokenURI     string `json:"token_uri"`
		} `json:"installed"`
	}
	var token struct {
		ClientID     string          `json:"client_id"`
		ClientSecret string          `json:"client_secret"`
		RefreshToken string          `json:"refresh_token"`
		Scopes       json.RawMessage `json:"scopes"`
	}
	if err := json.Unmarshal(clientData, &client); err != nil {
		return fmt.Errorf("decode installed OAuth client: %w", err)
	}
	if err := json.Unmarshal(tokenData, &token); err != nil {
		return fmt.Errorf("decode authorized-user token: %w", err)
	}
	connection := Connection{SpreadsheetID: strings.TrimSpace(input.SpreadsheetID), Tab: strings.TrimSpace(input.Tab), ClientID: strings.TrimSpace(client.Installed.ClientID), ClientSecret: client.Installed.ClientSecret, TokenURI: strings.TrimSpace(client.Installed.TokenURI), RefreshToken: strings.TrimSpace(token.RefreshToken)}
	if err := s.validate(&connection); err != nil {
		return err
	}
	if token.ClientID != "" && token.ClientID != connection.ClientID {
		return fmt.Errorf("authorized-user token client_id does not match installed client")
	}
	if token.ClientSecret != "" && token.ClientSecret != connection.ClientSecret {
		return fmt.Errorf("authorized-user token client_secret does not match installed client")
	}
	scopes, err := authorizedUserScopes(token.Scopes)
	if err != nil {
		return err
	}
	if !hasSheetsWriteScope(scopes) {
		return fmt.Errorf("authorized-user token lacks Google Sheets write scope")
	}
	return s.Write(&connection)
}

func authorizedUserScopes(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("authorized-user token lacks scopes")
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, fmt.Errorf("authorized-user token scopes are invalid")
	}
	return strings.Fields(text), nil
}

func hasSheetsWriteScope(scopes []string) bool {
	for _, scope := range scopes {
		if scope == "https://www.googleapis.com/auth/spreadsheets" {
			return true
		}
	}
	return false
}

func (s ConnectionStore) validate(connection *Connection) error {
	return validateConnection(connection, s.allowInsecureTokenURI)
}

func validateConnection(connection *Connection, allowInsecureTokenURI bool) error {
	if connection.SpreadsheetID == "" || connection.Tab == "" {
		return fmt.Errorf("spreadsheet ID and worksheet tab are required")
	}
	if connection.ClientID == "" || connection.ClientSecret == "" || connection.TokenURI == "" || connection.RefreshToken == "" {
		return fmt.Errorf("OAuth client or refresh token is incomplete")
	}
	parsed, err := url.ParseRequestURI(connection.TokenURI)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("OAuth token URI is invalid")
	}
	if allowInsecureTokenURI {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("OAuth token URI is invalid")
		}
		return nil
	}
	if parsed.Scheme != "https" || parsed.Host != "oauth2.googleapis.com" || parsed.Path != "/token" {
		return fmt.Errorf("OAuth token URI is invalid")
	}
	return nil
}

func (s ConnectionStore) Write(connection *Connection) error {
	if err := s.validate(connection); err != nil {
		return err
	}
	root, err := s.root()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create protected connection storage: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return fmt.Errorf("protect connection storage: %w", err)
	}
	path := filepath.Join(root, connectionFileName)
	if err := stateio.WriteJSONAtomic(path, connection); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (s ConnectionStore) Read() (Connection, error) {
	path, err := s.path()
	if err != nil {
		return Connection{}, err
	}
	root, err := s.root()
	if err != nil {
		return Connection{}, err
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return Connection{}, err
	}
	if !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 {
		return Connection{}, fmt.Errorf("development-audit connection storage is not user-only")
	}
	info, err := os.Stat(path)
	if err != nil {
		return Connection{}, err
	}
	if info.Mode().Perm() != 0o600 {
		return Connection{}, fmt.Errorf("development-audit connection record is not user-only")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed protected connection path.
	if err != nil {
		return Connection{}, err
	}
	var connection Connection
	if err := json.Unmarshal(data, &connection); err != nil {
		return Connection{}, fmt.Errorf("decode development-audit connection: %w", err)
	}
	if err := s.validate(&connection); err != nil {
		return Connection{}, err
	}
	return connection, nil
}

// SheetsReporter makes the small direct REST boundary testable without a
// network service or a generalized sink abstraction.
type SheetsReporter struct {
	Store         ConnectionStore
	HTTPClient    *http.Client
	SheetsBaseURL string
}

var defaultSheetsReporter = SheetsReporter{Store: ConnectionStore{}}

func (r SheetsReporter) Deliver(ctx context.Context, report *LocalReport) error {
	if report == nil {
		return fmt.Errorf("local report is required")
	}
	if report.Destination.State != "configured" || report.Destination.SpreadsheetID == "" || report.Destination.Tab == "" {
		return fmt.Errorf("reporting destination is %s", report.Destination.State)
	}
	connection, err := r.Store.Read()
	if err != nil {
		return fmt.Errorf("read reporting connection: %w", err)
	}
	lockContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	unlock, err := r.lock(lockContext, report.Destination)
	if err != nil {
		return err
	}
	defer unlock()
	accessToken, err := r.refresh(ctx, &connection)
	if err != nil {
		return err
	}
	if err := r.validateHeader(ctx, accessToken, report.Destination); err != nil {
		return err
	}
	existing, err := r.existingObservationIDs(ctx, accessToken, report.Destination)
	if err != nil {
		return err
	}
	rows := make([][]string, 0, len(report.Values.Observations))
	for index := range report.Values.Observations {
		observation := &report.Values.Observations[index]
		if _, found := existing[observation.ObservationID]; found {
			continue
		}
		row, err := projectObservation(observation)
		if err != nil {
			return fmt.Errorf("project observation %q: %w", observation.ObservationID, err)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		report.DeliveryState = "delivered"
		return nil
	}
	if err := r.append(ctx, accessToken, report.Destination, rows); err != nil {
		// A response can be lost after Google has committed it. Confirm each ID
		// before allowing a caller to retry; never append blindly on ambiguity.
		after, readErr := r.existingObservationIDs(ctx, accessToken, report.Destination)
		if readErr == nil && allRowsPresent(rows, after) {
			report.DeliveryState = "delivered"
			return nil
		}
		return err
	}
	report.DeliveryState = "delivered"
	return nil
}

func (r SheetsReporter) client() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return &http.Client{Timeout: 20 * time.Second}
}
func (r SheetsReporter) baseURL() string {
	if r.SheetsBaseURL != "" {
		return strings.TrimRight(r.SheetsBaseURL, "/")
	}
	return "https://sheets.googleapis.com/v4"
}

func (r SheetsReporter) refresh(ctx context.Context, connection *Connection) (string, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {connection.RefreshToken}, "client_id": {connection.ClientID}, "client_secret": {connection.ClientSecret}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, connection.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := r.client().Do(request)
	if err != nil {
		return "", fmt.Errorf("refresh OAuth token: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("refresh OAuth token: HTTP %d", response.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token); err != nil {
		return "", fmt.Errorf("decode OAuth token: %w", err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("OAuth token response lacks access_token")
	}
	return token.AccessToken, nil
}

func (r SheetsReporter) validateHeader(ctx context.Context, token string, destination DestinationState) error {
	var response struct {
		Values [][]string `json:"values"`
	}
	if err := r.getJSON(ctx, token, destination, a1Range(destination.Tab, "1:1"), &response); err != nil {
		return err
	}
	if len(response.Values) != 1 || !equalStrings(response.Values[0], stepValueHeader) {
		return fmt.Errorf("worksheet header does not match step_value_v1")
	}
	return nil
}

func (r SheetsReporter) existingObservationIDs(ctx context.Context, token string, destination DestinationState) (map[string]struct{}, error) {
	var response struct {
		Values [][]string `json:"values"`
	}
	if err := r.getJSON(ctx, token, destination, a1Range(destination.Tab, "B:B"), &response); err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(response.Values))
	for _, values := range response.Values {
		if len(values) > 0 && values[0] != "" && values[0] != "observation_id" {
			ids[values[0]] = struct{}{}
		}
	}
	return ids, nil
}

func (r SheetsReporter) getJSON(ctx context.Context, token string, destination DestinationState, cellRange string, out any) error {
	endpoint := r.baseURL() + "/spreadsheets/" + url.PathEscape(destination.SpreadsheetID) + "/values/" + url.PathEscape(cellRange)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := r.client().Do(request)
	if err != nil {
		return fmt.Errorf("read worksheet: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("read worksheet: HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode worksheet response: %w", err)
	}
	return nil
}

func (r SheetsReporter) append(ctx context.Context, token string, destination DestinationState, rows [][]string) error {
	body, err := json.Marshal(struct {
		Values [][]string `json:"values"`
	}{Values: rows})
	if err != nil {
		return err
	}
	endpoint := r.baseURL() + "/spreadsheets/" + url.PathEscape(destination.SpreadsheetID) + "/values/" + url.PathEscape(a1Range(destination.Tab, "A:AE")) + ":append?valueInputOption=RAW&insertDataOption=INSERT_ROWS"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client().Do(request)
	if err != nil {
		return fmt.Errorf("append worksheet rows: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("append worksheet rows: HTTP %d", response.StatusCode)
	}
	return nil
}

func (r SheetsReporter) lock(ctx context.Context, destination DestinationState) (func(), error) {
	root, err := r.Store.root()
	if err != nil {
		return nil, err
	}
	lockDir := filepath.Join(root, "development-audit-locks")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(lockDir, 0o700); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(destination.SpreadsheetID + "\x00" + destination.Tab))
	path := filepath.Join(lockDir, hex.EncodeToString(sum[:])+".lock")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600) // #nosec G304 -- hashed private destination lock path.
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
			}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func a1Range(tab, cells string) string {
	return "'" + strings.ReplaceAll(tab, "'", "''") + "'!" + cells
}

func projectObservation(observation *ValueObservation) ([]string, error) {
	if err := safeValueNote(observation.Note); err != nil {
		return nil, err
	}
	if observation.SchemaVersion != valueSchemaVersion {
		return nil, fmt.Errorf("unsupported schema %q", observation.SchemaVersion)
	}
	models, commits := sortedJoined(observation.Cost.SourceModels), sortedJoined(observation.Git.CommitSHAs)
	return []string{
		observation.SchemaVersion, observation.ObservationID, observation.ObservedAtUTC, sanitizeProject(observation.Project), observation.Workflow, observation.SourceRunID, observation.ExecutionSessionID, observation.AuditRunID, observation.Trigger, observation.SourceOutcome, observation.StepID, observation.StepOutcome, observation.Lineage,
		intString(observation.Cost.DurationMS), floatString(observation.Cost.CostUSD), intString(observation.Cost.TotalTokens), models, observation.Git.Attribution, commits, intString(observation.Git.FilesChanged), intString(observation.Git.LinesAdded), intString(observation.Git.LinesDeleted),
		observation.OverallValue, observation.ChangeEffect, observation.UniqueContribution, observation.DownstreamEvidence, observation.Confidence, observation.EvidenceCoverage, observation.JudgeModel, observation.RubricVersion, observation.Note,
	}, nil
}

func sanitizeProject(project string) string {
	project = strings.TrimSpace(project)
	parts := strings.Split(project, "/")
	if len(parts) == 2 && validProjectPart(parts[0]) && validProjectPart(parts[1]) {
		return project
	}
	base := filepath.Base(strings.TrimRight(project, "/\\"))
	if validProjectPart(base) {
		return base
	}
	return "unknown"
}

func projectForRepository(root string) string {
	if strings.TrimSpace(root) == "" {
		return "unknown"
	}
	command := exec.Command("git", "-C", root, "config", "--get-regexp", `^remote\..*\.url$`) // #nosec G204 -- fixed git query against Runner-owned project root.
	output, err := command.Output()
	if err != nil {
		return projectFromRemote("", root)
	}
	remotes := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			remotes = append(remotes, fields[1])
		}
	}
	sort.Strings(remotes)
	for _, remote := range remotes {
		if project, ok := projectSlugFromRemote(remote); ok {
			return project
		}
	}
	return projectFromRemote("", root)
}

func projectFromRemote(remote, root string) string {
	if project, ok := projectSlugFromRemote(remote); ok {
		return project
	}
	return sanitizeProject(filepath.Base(root))
}

func projectSlugFromRemote(remote string) (string, bool) {
	remote = strings.TrimSpace(remote)
	if strings.HasPrefix(remote, "git@") {
		if colon := strings.IndexByte(remote, ':'); colon >= 0 {
			remote = remote[colon+1:]
		} else {
			return "", false
		}
	} else {
		parsed, err := url.Parse(remote)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "ssh" && parsed.Scheme != "git") {
			return "", false
		}
		remote = parsed.Path
	}
	remote = strings.TrimSuffix(strings.Trim(remote, "/"), ".git")
	parts := strings.Split(remote, "/")
	if len(parts) >= 2 {
		candidate := parts[len(parts)-2] + "/" + parts[len(parts)-1]
		if sanitizeProject(candidate) == candidate {
			return candidate, true
		}
	}
	return "", false
}
func validProjectPart(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char != '-' && char != '_' && char != '.' && (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}
func sortedJoined(values []string) string {
	values = append([]string(nil), values...)
	sort.Strings(values)
	return strings.Join(values, ",")
}
func intString(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}
func floatString(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}
func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
func allRowsPresent(rows [][]string, present map[string]struct{}) bool {
	for _, row := range rows {
		if len(row) < 2 {
			return false
		}
		if _, ok := present[row[1]]; !ok {
			return false
		}
	}
	return true
}

func reportValueObservationsStage(request Request) error {
	path := filepath.Join(request.AuditSessionDir, "local-report.json")
	data, err := os.ReadFile(path) // #nosec G304 -- fixed audit-owned report path.
	if err != nil {
		return err
	}
	var report LocalReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("decode local report: %w", err)
	}
	if err := defaultSheetsReporter.Deliver(context.Background(), &report); err != nil {
		report.DeliveryState = "pending"
		if writeErr := stateio.WriteJSONAtomic(path, report); writeErr != nil {
			return writeErr
		}
		// Sheets is deliberately a non-blocking final stage. Preserve its reason
		// for status/retry but do not make the linked audit or source fail.
		if request.SourceSessionDir != "" {
			_ = RecordReportingWarning(request, "Sheets reporting pending: "+err.Error())
		}
		return nil
	}
	return stateio.WriteJSONAtomic(path, report)
}

// RetryReport delivers a completed local report only. It does not reconstruct
// evidence or invoke either audit model, so observation identities stay fixed.
func RetryReport(auditSessionDir string) error {
	data, err := os.ReadFile(filepath.Join(auditSessionDir, "local-report.json")) // #nosec G304 -- caller-selected audit directory contains a fixed artifact name.
	if err != nil {
		return err
	}
	var report LocalReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("decode local report: %w", err)
	}
	if err := defaultSheetsReporter.Deliver(context.Background(), &report); err != nil {
		return err
	}
	return stateio.WriteJSONAtomic(filepath.Join(auditSessionDir, "local-report.json"), report)
}

// MigrateReportDestination deliberately replaces the destination frozen in a
// completed report. Normal retries never call this; migration is an explicit
// operator action for a report that has not yet been delivered.
func MigrateReportDestination(auditSessionDir, spreadsheetID, tab string) error {
	if strings.TrimSpace(spreadsheetID) == "" || strings.TrimSpace(tab) == "" {
		return fmt.Errorf("migration spreadsheet ID and worksheet tab are required")
	}
	path := filepath.Join(auditSessionDir, "local-report.json")
	data, err := os.ReadFile(path) // #nosec G304 -- caller-selected audit directory contains a fixed artifact name.
	if err != nil {
		return err
	}
	var report LocalReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("decode local report: %w", err)
	}
	if report.SchemaVersion != valueSchemaVersion {
		return fmt.Errorf("report schema %q cannot be migrated", report.SchemaVersion)
	}
	report.Destination = DestinationState{State: "configured", SpreadsheetID: strings.TrimSpace(spreadsheetID), Tab: strings.TrimSpace(tab)}
	report.DeliveryState = "pending"
	return stateio.WriteJSONAtomic(path, report)
}
