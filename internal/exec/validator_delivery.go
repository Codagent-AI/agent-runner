package exec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/codagent/agent-runner/internal/measurements"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

type validatorMetricsBatch struct {
	Records      []json.RawMessage `json:"records"`
	Receipt      string            `json:"receipt"`
	Acknowledged bool              `json:"acknowledged"`
}
type validatorMetricsSink interface {
	IncorporateValidator(metrics.Attribution, string, []json.RawMessage, metrics.DeliveryContext) error
}

// Calls within one process serialize context transactions. Cross-process
// recovery additionally takes the existing run lock at the CLI boundary.
var validatorDeliveryMu sync.Mutex

func emitNestedMetricCapture(ctx *model.ExecutionContext, _ *model.Step, _ string, capture *nestedMetricsCapture) {
	if capture == nil || capture.path == "" {
		return
	}
	if err := deliverValidatorContext(ctx, capture.path); err != nil {
		fmt.Fprintln(os.Stderr, "agent-runner: warning: validator metrics delivery incomplete; run metrics recover after resolving the delivery gap")
	}
}
func readValidatorMetricsLaunch(path string) (validatorMetricsLaunch, error) {
	var launch validatorMetricsLaunch
	raw, err := os.ReadFile(path) // #nosec G304 -- fixed context journal found under the run directory.
	if err != nil {
		return launch, err
	}
	if json.Unmarshal(raw, &launch) != nil || launch.Consumer != "agent-runner" || launch.ContextID == "" || launch.Project == "" {
		return launch, fmt.Errorf("invalid_validator_launch")
	}
	return launch, nil
}
func (launch *validatorMetricsLaunch) attribution() metrics.Attribution {
	return metrics.Attribution{RunID: launch.RunID, ExecutionSessionID: launch.ExecutionSessionID, ParentAttemptID: launch.ParentAttemptID, StepID: launch.StepID, Prefix: launch.Prefix, ContextID: launch.ContextID}
}

// RecoverValidatorMetrics only delivers persisted contexts. It never launches
// steps or creates an execution session, including on completed runs.
func RecoverValidatorMetrics(ctx *model.ExecutionContext) error {
	paths, err := filepath.Glob(filepath.Join(ctx.SessionDir, "validator-metrics", "contexts", "*.json"))
	if err != nil {
		return err
	}
	var failures []error
	for _, path := range paths {
		if err := deliverValidatorContext(ctx, path); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func deliverValidatorContext(ctx *model.ExecutionContext, path string) error {
	validatorDeliveryMu.Lock()
	defer validatorDeliveryMu.Unlock()
	launch, err := readValidatorMetricsLaunch(path)
	if err != nil {
		return err
	}
	sink, ok := ctx.AuditLogger.(validatorMetricsSink)
	if !ok {
		return fmt.Errorf("durable_metrics_sink_unavailable")
	}
	attr := launch.attribution()
	delivery := launch.Delivery
	delivery.Attribution = attr
	fail := func(code string) error {
		delivery.Delivery = "blocked"
		delivery.Gaps = []string{code}
		delivery.History = "partial"
		launch.Delivery = delivery
		// Neither failure can authorize acknowledgment; retain both local warnings.
		saveErr := stateio.WriteJSONDurable(path, launch)
		projectErr := sink.IncorporateValidator(attr, launch.StoreID, nil, delivery)
		return errors.Join(fmt.Errorf("%s", code), saveErr, projectErr)
	}
	if err := replayValidatorBatches(&launch, path, sink); err != nil {
		return fail(err.Error())
	}
	// Finite drain bound protects finalization from an indefinitely active producer.
	for batchNumber := 0; batchNumber < 32; batchNumber++ {
		response, err := exportValidatorMetrics(&launch)
		if err != nil {
			return fail(deliveryErrorCode(err))
		}
		if launch.StoreID != "" && launch.StoreID != response.StoreID {
			return fail("original_store_replaced")
		}
		launch.StoreID = response.StoreID
		delivery = metrics.DeliveryContext{Attribution: attr, StoreID: launch.StoreID, EvidenceState: response.EvidenceState, Delivery: "pending", History: "complete", Generation: response.Batch.Generation, ScopeComplete: response.Batch.ScopeComplete, Gaps: append([]string{}, response.Gaps.Reasons...)}
		if response.Gaps.Count > 0 || response.EvidenceState == "discarded" {
			delivery.History = "partial"
			delivery.Gaps = append(delivery.Gaps, "discarded_evidence")
		}
		if response.EvidenceState == "missing" {
			delivery.History = "partial"
			delivery.Gaps = append(delivery.Gaps, "missing_evidence")
		}
		launch.Delivery = delivery
		if len(response.Records) == 0 {
			delivery.Delivery = "complete"
			if len(delivery.Gaps) > 0 {
				delivery.Delivery = "blocked"
			}
			launch.Delivery = delivery
			if err = stateio.WriteJSONDurable(path, launch); err != nil {
				return fail("journal_save_failed")
			}
			if err = sink.IncorporateValidator(attr, launch.StoreID, nil, delivery); err != nil {
				return fail("projection_save_failed")
			}
			if completed, ok := sink.(interface{ ValidatorContextComplete(string) bool }); ok && !completed.ValidatorContextComplete(launch.ContextID) {
				return fmt.Errorf("collection_incomplete")
			}
			if delivery.Delivery != "complete" || !delivery.ScopeComplete {
				return fmt.Errorf("delivery_incomplete")
			}
			return nil
		}
		if err := persistValidatorBatch(&launch, path, sink, &response); err != nil {
			return fail(err.Error())
		}
	}
	return fail("delivery_batch_limit")
}

type validatorExport struct {
	MeasurementVersions []int  `json:"measurement_schema_versions"`
	OK                  bool   `json:"ok"`
	Operation           string `json:"operation"`
	ProtocolVersion     int    `json:"protocol_version"`
	Producer            struct {
		Name string `json:"name"`
	} `json:"producer"`
	Diagnostics     []string             `json:"diagnostics"`
	StoreID         string               `json:"store_id"`
	ConsumerContext measurements.Context `json:"consumer_context"`
	ExportID        *string              `json:"export_id"`
	EvidenceState   string               `json:"evidence_state"`
	Records         []json.RawMessage    `json:"records"`
	Receipt         *string              `json:"receipt"`
	Batch           struct {
		Generation    int64 `json:"generation"`
		Returned      int   `json:"returned_revision_count"`
		Remaining     int   `json:"remaining_revision_count"`
		ScopeComplete bool  `json:"scope_complete"`
	} `json:"batch"`
	Gaps struct {
		Count   int      `json:"count"`
		Reasons []string `json:"reasons"`
	} `json:"delivery_gaps"`
}

func protocolDecode(raw []byte, target any) error {
	if len(raw) > 4000000 {
		return fmt.Errorf("export_byte_limit")
	}
	if _, err := measurements.Decode(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return fmt.Errorf("invalid_protocol_response")
	}
	return nil
}
func exportValidatorMetrics(launch *validatorMetricsLaunch) (validatorExport, error) {
	var response validatorExport
	args := validatorScopeArgs(launch, "export")
	args = append(args, "--measurement-version", "1", "--max-records", "100", "--max-bytes", "1000000")
	raw, err := runValidatorDataCommand(launch, args...)
	if err != nil {
		return response, producerError(raw)
	}
	if err := protocolDecode(raw, &response); err != nil {
		return response, err
	}
	if !response.OK || response.Operation != "export" || response.ProtocolVersion != 1 || response.Producer.Name != "agent-validator" || response.StoreID == "" || response.ConsumerContext.Consumer != launch.Consumer || response.ConsumerContext.ContextID != launch.ContextID {
		return response, fmt.Errorf("invalid_export_scope")
	}
	if response.Batch.Returned != len(response.Records) || len(response.Records) > 100 || response.Batch.Generation < 0 || response.Batch.Remaining < 0 || response.Batch.ScopeComplete != (response.Batch.Remaining == 0) || response.Gaps.Count < 0 {
		return response, fmt.Errorf("invalid_batch")
	}
	switch response.EvidenceState {
	case "pending", "previously_acknowledged", "discarded", "missing":
	default:
		return response, fmt.Errorf("invalid_evidence_state")
	}
	for _, version := range response.MeasurementVersions {
		if version != 1 {
			return response, fmt.Errorf("unsupported_measurement_version")
		}
	}
	for _, raw := range response.Records {
		if err := validateValidatorRecord(raw, launch.ContextID); err != nil {
			return response, err
		}
	}
	if len(response.Records) > 0 && (response.Receipt == nil || *response.Receipt == "") {
		return response, fmt.Errorf("missing_receipt")
	}
	return response, nil
}
func validateValidatorRecord(raw json.RawMessage, contextID string) error {
	_, err := measurements.ValidateRecord(raw, "agent-runner", contextID)
	return err
}
func validatorScopeArgs(launch *validatorMetricsLaunch, operation string) []string {
	args := []string{"metrics", operation, "--project", launch.Project, "--consumer", launch.Consumer, "--context", launch.ContextID, "--protocol-version", "1"}
	if launch.Configuration != "" {
		args = append(args, "--config", launch.Configuration)
	}
	return args
}
func acknowledgeValidatorReceipt(launch *validatorMetricsLaunch, receipt string) error {
	var response struct {
		OK              bool   `json:"ok"`
		Operation       string `json:"operation"`
		ProtocolVersion int    `json:"protocol_version"`
		Producer        struct {
			Name string `json:"name"`
		} `json:"producer"`
		Diagnostics []string `json:"diagnostics"`
		Receipt     string   `json:"receipt"`
		Disposition string   `json:"disposition"`
	}
	raw, err := runValidatorDataCommand(launch, append(validatorScopeArgs(launch, "acknowledge"), "--receipt", receipt)...)
	if err != nil {
		return producerError(raw)
	}
	if protocolDecode(raw, &response) != nil || !response.OK || response.Producer.Name != "agent-validator" || response.Operation != "acknowledge" || response.ProtocolVersion != 1 || response.Receipt != receipt || response.Disposition != "acknowledged" {
		return fmt.Errorf("invalid_acknowledgment")
	}
	return nil
}
func producerError(raw []byte) error {
	var response struct {
		Error struct {
			Code     string `json:"code"`
			Required []int  `json:"required_measurement_schema_versions"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &response)
	if response.Error.Code == "unsupported_measurement_version" || response.Error.Code == "unsupported_version" && len(response.Error.Required) > 0 {
		return fmt.Errorf("unsupported_measurement_version")
	}
	return fmt.Errorf("producer_command_failed")
}
func deliveryErrorCode(err error) string {
	if err.Error() == "unsupported_measurement_version" {
		return err.Error()
	}
	return "export_rejected"
}

func replayValidatorBatches(launch *validatorMetricsLaunch, path string, sink validatorMetricsSink) error {
	attr := launch.attribution()
	delivery := launch.Delivery
	var err error
	// Rehydrate the projection even if it was deleted after prior acknowledgment.
	// Legacy journals are replayed through the same verifier before any receipt.
	if len(launch.AcceptedRecords) > 0 {
		launch.Batches = append(launch.Batches, validatorMetricsBatch{Records: launch.AcceptedRecords, Receipt: launch.Receipt, Acknowledged: launch.Acknowledged})
		launch.AcceptedRecords = nil
	}
	for _, batch := range launch.Batches {
		if err = sink.IncorporateValidator(attr, launch.StoreID, batch.Records, delivery); err != nil {
			return fmt.Errorf("projection_replay_failed")
		}
	}
	if launch.Executable == "" {
		return fmt.Errorf("original_executable_unavailable")
	}
	if launch.ConfigurationHash != "" && configurationFingerprint(launch.Project, launch.Configuration) != launch.ConfigurationHash {
		return fmt.Errorf("original_configuration_changed")
	}
	for i := range launch.Batches {
		batch := &launch.Batches[i]
		if batch.Acknowledged {
			continue
		}
		if err = acknowledgeValidatorReceipt(launch, batch.Receipt); err != nil {
			return fmt.Errorf("acknowledgment_unconfirmed")
		}
		batch.Acknowledged = true
		if err = stateio.WriteJSONDurable(path, launch); err != nil {
			return fmt.Errorf("acknowledgment_save_failed")
		}
	}
	return nil
}
func persistValidatorBatch(launch *validatorMetricsLaunch, path string, sink validatorMetricsSink, response *validatorExport) error {
	attr := launch.attribution()
	delivery := launch.Delivery
	var err error
	batch := validatorMetricsBatch{Records: response.Records, Receipt: *response.Receipt}
	launch.Batches = append(launch.Batches, batch)
	if err = stateio.WriteJSONDurable(path, launch); err != nil {
		return fmt.Errorf("journal_save_failed")
	}
	if err = sink.IncorporateValidator(attr, launch.StoreID, batch.Records, delivery); err != nil {
		return fmt.Errorf("projection_save_failed")
	}
	if err = acknowledgeValidatorReceipt(launch, batch.Receipt); err != nil {
		return fmt.Errorf("acknowledgment_unconfirmed")
	}
	launch.Batches[len(launch.Batches)-1].Acknowledged = true
	if err = stateio.WriteJSONDurable(path, launch); err != nil {
		return fmt.Errorf("acknowledgment_save_failed")
	}
	return nil
}

func runValidatorDataCommand(launch *validatorMetricsLaunch, args ...string) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		raw, err := runValidatorMetricsCommand(launch, args...)
		if err == nil || attempt == 2 {
			return raw, err
		}
		var response struct {
			Error struct {
				Code      string `json:"code"`
				Retryable bool   `json:"retryable"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &response) != nil || response.Error.Code != "store_busy" || !response.Error.Retryable {
			return raw, err
		}
	}
}
