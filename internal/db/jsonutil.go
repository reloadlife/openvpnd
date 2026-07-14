package db

import (
	"encoding/json"
	"strings"
)

func encodeJSONList(items []string) string {
	if items == nil {
		items = []string{}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeJSONList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func encodeRemotes(remotes []Remote) string {
	if remotes == nil {
		remotes = []Remote{}
	}
	b, err := json.Marshal(remotes)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeRemotes(s string) []Remote {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return []Remote{}
	}
	var out []Remote
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []Remote{}
	}
	if out == nil {
		return []Remote{}
	}
	return out
}
