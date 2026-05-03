package model

import (
	"encoding/json"
	"fmt"
)

type CaptureKind string

const (
	CaptureString CaptureKind = "string"
	CaptureList   CaptureKind = "list"
	CaptureMap    CaptureKind = "map"
)

type CapturedValue struct {
	Kind CaptureKind
	Str  string
	List []string
	Map  map[string]string
}

func NewCapturedString(s string) CapturedValue {
	return CapturedValue{Kind: CaptureString, Str: s}
}

func NewCapturedList(values []string) CapturedValue {
	cp := append([]string(nil), values...)
	return CapturedValue{Kind: CaptureList, List: cp}
}

func NewCapturedMap(values map[string]string) CapturedValue {
	cp := make(map[string]string, len(values))
	for k, v := range values {
		cp[k] = v
	}
	return CapturedValue{Kind: CaptureMap, Map: cp}
}

func (v CapturedValue) StringValue() (string, bool) {
	if v.Kind != CaptureString {
		return "", false
	}
	return v.Str, true
}

func (v CapturedValue) AuditValue() any {
	switch v.Kind {
	case CaptureList:
		return append([]string(nil), v.List...)
	case CaptureMap:
		return NewCapturedMap(v.Map).Map
	default:
		return v.Str
	}
}

func (v CapturedValue) MarshalJSON() ([]byte, error) {
	switch v.Kind {
	case CaptureString:
		return json.Marshal(capturedValueEnvelope[string]{Kind: CaptureString, Value: v.Str})
	case CaptureList:
		return json.Marshal(capturedValueEnvelope[[]string]{Kind: CaptureList, Value: v.List})
	case CaptureMap:
		return json.Marshal(capturedValueEnvelope[map[string]string]{Kind: CaptureMap, Value: v.Map})
	default:
		return nil, fmt.Errorf("unknown capture kind %q", v.Kind)
	}
}

func (v *CapturedValue) UnmarshalJSON(data []byte) error {
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		*v = NewCapturedString(legacy)
		return nil
	}

	var head struct {
		Kind  CaptureKind     `json:"kind"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	switch head.Kind {
	case CaptureString:
		var s string
		if err := json.Unmarshal(head.Value, &s); err != nil {
			return err
		}
		*v = NewCapturedString(s)
	case CaptureList:
		var values []string
		if err := json.Unmarshal(head.Value, &values); err != nil {
			return err
		}
		*v = NewCapturedList(values)
	case CaptureMap:
		var values map[string]string
		if err := json.Unmarshal(head.Value, &values); err != nil {
			return err
		}
		*v = NewCapturedMap(values)
	default:
		return fmt.Errorf("unknown capture kind %q", head.Kind)
	}
	return nil
}

type capturedValueEnvelope[T any] struct {
	Kind  CaptureKind `json:"kind"`
	Value T           `json:"value"`
}
