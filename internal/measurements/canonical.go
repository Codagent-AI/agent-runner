// Package measurements implements the pinned, closed measurement contract.
package measurements

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// Decode rejects duplicate keys, trailing data and invalid Unicode before any
// semantic interpretation. Errors deliberately contain no producer content.
func Decode(raw []byte) (any, error) {
	if !utf8.Valid(raw) || !validSurrogates(raw) {
		return nil, fmt.Errorf("invalid_unicode")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	value, err := decodeValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing_json")
	}
	return value, nil
}
func decodeValue(d *json.Decoder, depth int) (any, error) {
	if depth > 100 {
		return nil, fmt.Errorf("json_depth_limit")
	}
	token, err := d.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid_json")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		result := map[string]any{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return nil, fmt.Errorf("invalid_json")
			}
			k, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("invalid_key")
			}
			if _, exists := result[k]; exists {
				return nil, fmt.Errorf("duplicate_object_key")
			}
			value, err := decodeValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			result[k] = value
		}
		if _, err = d.Token(); err != nil {
			return nil, fmt.Errorf("invalid_json")
		}
		return result, nil
	case '[':
		result := []any{}
		for d.More() {
			value, err := decodeValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		if _, err = d.Token(); err != nil {
			return nil, fmt.Errorf("invalid_json")
		}
		return result, nil
	}
	return nil, fmt.Errorf("invalid_json")
}
func validSurrogates(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n < 0xd800 || n > 0xdbff {
			continue
		}
		if i+6 >= len(raw) || string(raw[i+1:i+3]) != "\\u" {
			return false
		}
		low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}

// CanonicalRecord returns RFC 8785 bytes with only the top-level digest omitted.
func CanonicalRecord(raw []byte) ([]byte, error) {
	value, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	if obj, ok := value.(map[string]any); ok {
		delete(obj, "digest")
	}
	var out bytes.Buffer
	if err := canonical(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func canonical(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
			for n := 0; n < len(a) && n < len(b); n++ {
				if a[n] != b[n] {
					return a[n] < b[n]
				}
			}
			return len(a) < len(b)
		})
		out.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeString(out, k)
			out.WriteByte(':')
			if err := canonical(out, v[k]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := canonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case string:
		writeString(out, v)
	case float64:
		if v == 0 {
			out.WriteByte('0')
			break
		}
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid_number")
		}
		out.Write(b)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("invalid_json")
		}
		out.Write(b)
	}
	return nil
}
func writeString(out *bytes.Buffer, s string) {
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}
