package cli

import (
	"fmt"
	"strconv"
)

var codexAgentCallTOMLPath = []string{"mcp_servers", agentCallMCPServerName}

// codexConfigHasAgentCallConflict scans TOML declarations structurally for
// the Runner-owned MCP path. It recognizes bare and quoted dotted keys and
// table headers while ignoring comments and string values. Keeping this
// narrow scanner in-tree avoids making every offline CLI invocation depend on
// a TOML module that may not be present in the validator's sealed module cache.
func codexConfigHasAgentCallConflict(config []byte) (bool, error) {
	scanner := tomlKeyScanner{data: config}
	return scanner.containsPath(codexAgentCallTOMLPath)
}

type tomlKeyScanner struct {
	data []byte
	pos  int
}

func (s *tomlKeyScanner) containsPath(target []string) (bool, error) {
	var table []string
	for {
		s.skipDocumentSpace()
		if s.pos >= len(s.data) {
			return false, nil
		}
		if s.data[s.pos] == '[' {
			path, arrayTable, err := s.parseTableHeader()
			if err != nil {
				return false, err
			}
			if tomlPathHasPrefix(path, target) || arrayTable && tomlPathsEqual(path, target[:1]) {
				return true, nil
			}
			table = path
			continue
		}

		key, err := s.parseKeyPath()
		if err != nil {
			return false, err
		}
		s.skipHorizontalSpace()
		if s.pos >= len(s.data) || s.data[s.pos] != '=' {
			return false, s.parseError("expected '=' after key")
		}
		s.pos++
		path := append(append([]string(nil), table...), key...)
		// Assigning mcp_servers as a value creates a sealed/scalar key that
		// cannot safely be extended with [mcp_servers.agent-runner].
		if tomlPathHasPrefix(path, target) || tomlPathsEqual(path, target[:1]) {
			return true, nil
		}
		if err := s.skipValue(); err != nil {
			return false, err
		}
	}
}

func (s *tomlKeyScanner) parseTableHeader() (path []string, arrayTable bool, err error) {
	s.pos++
	arrayTable = s.pos < len(s.data) && s.data[s.pos] == '['
	if arrayTable {
		s.pos++
	}
	s.skipHorizontalSpace()
	path, err = s.parseKeyPath()
	if err != nil {
		return nil, false, err
	}
	s.skipHorizontalSpace()
	if s.pos >= len(s.data) || s.data[s.pos] != ']' {
		return nil, false, s.parseError("unterminated table header")
	}
	s.pos++
	if arrayTable {
		if s.pos >= len(s.data) || s.data[s.pos] != ']' {
			return nil, false, s.parseError("unterminated array table header")
		}
		s.pos++
	}
	s.skipHorizontalSpace()
	if s.pos < len(s.data) && s.data[s.pos] == '#' {
		s.skipComment()
	}
	if s.pos < len(s.data) && s.data[s.pos] != '\n' && s.data[s.pos] != '\r' {
		return nil, false, s.parseError("unexpected content after table header")
	}
	return path, arrayTable, nil
}

func (s *tomlKeyScanner) parseKeyPath() ([]string, error) {
	var path []string
	for {
		s.skipHorizontalSpace()
		part, err := s.parseKeyPart()
		if err != nil {
			return nil, err
		}
		path = append(path, part)
		s.skipHorizontalSpace()
		if s.pos >= len(s.data) || s.data[s.pos] != '.' {
			return path, nil
		}
		s.pos++
	}
}

func (s *tomlKeyScanner) parseKeyPart() (string, error) {
	if s.pos >= len(s.data) {
		return "", s.parseError("missing key")
	}
	switch s.data[s.pos] {
	case '\'':
		start := s.pos + 1
		s.pos++
		for s.pos < len(s.data) && s.data[s.pos] != '\'' {
			if s.data[s.pos] == '\n' || s.data[s.pos] == '\r' {
				return "", s.parseError("newline in quoted key")
			}
			s.pos++
		}
		if s.pos >= len(s.data) {
			return "", s.parseError("unterminated quoted key")
		}
		part := string(s.data[start:s.pos])
		s.pos++
		return part, nil
	case '"':
		start := s.pos
		s.pos++
		for s.pos < len(s.data) {
			switch s.data[s.pos] {
			case '\\':
				s.pos += 2
			case '"':
				s.pos++
				part, err := strconv.Unquote(string(s.data[start:s.pos]))
				if err != nil {
					return "", s.parseError("invalid basic quoted key")
				}
				return part, nil
			case '\n', '\r':
				return "", s.parseError("newline in quoted key")
			default:
				s.pos++
			}
		}
		return "", s.parseError("unterminated quoted key")
	default:
		start := s.pos
		for s.pos < len(s.data) && isTOMLBareKeyByte(s.data[s.pos]) {
			s.pos++
		}
		if s.pos == start {
			return "", s.parseError("invalid bare key")
		}
		return string(s.data[start:s.pos]), nil
	}
}

func (s *tomlKeyScanner) skipValue() error {
	squareDepth, curlyDepth := 0, 0
	for s.pos < len(s.data) {
		switch s.data[s.pos] {
		case '\'', '"':
			if err := s.skipString(); err != nil {
				return err
			}
		case '#':
			s.skipComment()
			if squareDepth == 0 && curlyDepth == 0 {
				return nil
			}
		case '[':
			squareDepth++
			s.pos++
		case ']':
			if squareDepth == 0 {
				return s.parseError("unexpected ']' in value")
			}
			squareDepth--
			s.pos++
		case '{':
			curlyDepth++
			s.pos++
		case '}':
			if curlyDepth == 0 {
				return s.parseError("unexpected '}' in value")
			}
			curlyDepth--
			s.pos++
		case '\n', '\r':
			s.pos++
			if squareDepth == 0 && curlyDepth == 0 {
				return nil
			}
		default:
			s.pos++
		}
	}
	if squareDepth != 0 || curlyDepth != 0 {
		return s.parseError("unterminated composite value")
	}
	return nil
}

func (s *tomlKeyScanner) skipString() error {
	quote := s.data[s.pos]
	multiline := s.pos+2 < len(s.data) && s.data[s.pos+1] == quote && s.data[s.pos+2] == quote
	if multiline {
		s.pos += 3
	} else {
		s.pos++
	}
	for s.pos < len(s.data) {
		if multiline && s.pos+2 < len(s.data) && s.data[s.pos] == quote && s.data[s.pos+1] == quote && s.data[s.pos+2] == quote {
			s.pos += 3
			return nil
		}
		if !multiline && s.data[s.pos] == quote {
			s.pos++
			return nil
		}
		if quote == '"' && s.data[s.pos] == '\\' {
			s.pos += 2
			continue
		}
		if !multiline && (s.data[s.pos] == '\n' || s.data[s.pos] == '\r') {
			return s.parseError("newline in string")
		}
		s.pos++
	}
	return s.parseError("unterminated string")
}

func (s *tomlKeyScanner) skipDocumentSpace() {
	for s.pos < len(s.data) {
		switch s.data[s.pos] {
		case ' ', '\t', '\n', '\r':
			s.pos++
		case '#':
			s.skipComment()
		default:
			return
		}
	}
}

func (s *tomlKeyScanner) skipHorizontalSpace() {
	for s.pos < len(s.data) && (s.data[s.pos] == ' ' || s.data[s.pos] == '\t') {
		s.pos++
	}
}

func (s *tomlKeyScanner) skipComment() {
	for s.pos < len(s.data) && s.data[s.pos] != '\n' {
		s.pos++
	}
}

func (s *tomlKeyScanner) parseError(message string) error {
	return fmt.Errorf("parse Codex config before installing Runner integration near byte %d: %s", s.pos, message)
}

func isTOMLBareKeyByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '-'
}

func tomlPathHasPrefix(path, target []string) bool {
	if len(path) < len(target) {
		return false
	}
	for i := range target {
		if path[i] != target[i] {
			return false
		}
	}
	return true
}

func tomlPathsEqual(left, right []string) bool {
	return len(left) == len(right) && tomlPathHasPrefix(left, right)
}
