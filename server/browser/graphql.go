package browser

import (
	"encoding/json"
	"strings"
)

// operation is one GraphQL operation definition: its type and name.
type operation struct{ typ, name string }

// operations reads a GraphQL document's operation definitions (GraphQL spec §2.3):
// strings, block strings and comments are skipped; at the top level, "query",
// "mutation" and "subscription" open an operation, "fragment" a fragment, and a bare
// selection set is the query shorthand.
func operations(doc string) ([]operation, bool) {
	var out []operation
	depth := 0
	i := 0
	n := len(doc)
	word := func() string {
		j := i
		for j < n && (doc[j] == '_' || doc[j] >= 'a' && doc[j] <= 'z' || doc[j] >= 'A' && doc[j] <= 'Z' || doc[j] >= '0' && doc[j] <= '9') {
			j++
		}
		w := doc[i:j]
		i = j
		return w
	}
	space := func() {
		for i < n {
			switch {
			case doc[i] == '#':
				for i < n && doc[i] != '\n' {
					i++
				}
			case doc[i] == ' ' || doc[i] == '\t' || doc[i] == '\n' || doc[i] == '\r' || doc[i] == ',':
				i++
			default:
				return
			}
		}
	}
	for i < n {
		space()
		if i >= n {
			break
		}
		c := doc[i]
		switch {
		case strings.HasPrefix(doc[i:], `"""`):
			k := strings.Index(doc[i+3:], `"""`)
			if k < 0 {
				return nil, false
			}
			i += k + 6
		case c == '"':
			i++
			for i < n && doc[i] != '"' {
				if doc[i] == '\\' {
					i++
				}
				i++
			}
			i++
		case c == '{':
			if depth == 0 {
				out = append(out, operation{typ: "query"}) // the shorthand
			}
			depth++
			i++
		case c == '}':
			depth--
			i++
		case depth == 0 && (c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'):
			w := word()
			switch w {
			case "query", "mutation", "subscription":
				space()
				name := word()
				out = append(out, operation{typ: w, name: name})
				// Into the operation's selection set (not a shorthand of its own).
				for i < n && doc[i] != '{' {
					if doc[i] == '"' {
						i++
						for i < n && doc[i] != '"' {
							i++
						}
					}
					i++
				}
				if i < n {
					depth++
					i++
				}
			case "fragment":
				for i < n && doc[i] != '{' {
					i++
				}
				if i < n {
					depth++
					i++
				}
			default:
				return nil, false // not a document this reads
			}
		default:
			i++
		}
	}
	return out, depth == 0
}

// graphqlRead reports a body whose every request (one, or each entry of a batch) carries
// its document in its bytes and selects only queries.
func graphqlRead(body []byte, contentType string) bool {
	if strings.Contains(contentType, "application/graphql") {
		ops, ok := operations(string(body))
		return ok && selected(ops, "") == "query"
	}
	var batch []map[string]json.RawMessage
	if json.Unmarshal(body, &batch) != nil {
		var one map[string]json.RawMessage
		if json.Unmarshal(body, &one) != nil {
			return false
		}
		batch = []map[string]json.RawMessage{one}
	}
	if len(batch) == 0 {
		return false
	}
	for _, r := range batch {
		var q, name string
		if json.Unmarshal(r["query"], &q) != nil || q == "" {
			return false // a persisted document: the bytes don't show what it does
		}
		json.Unmarshal(r["operationName"], &name)
		ops, ok := operations(q)
		if !ok || selected(ops, name) != "query" {
			return false
		}
	}
	return true
}

// selected is the type of the operation a request selects: by name, or the only one.
func selected(ops []operation, name string) string {
	if name == "" {
		if len(ops) == 1 {
			return ops[0].typ
		}
		return ""
	}
	for _, o := range ops {
		if o.name == name {
			return o.typ
		}
	}
	return ""
}

// safe is RFC 9110 §9.2.1's safe methods, with QUERY (RFC 10008).
func safe(method string) bool {
	switch method {
	case "GET", "HEAD", "OPTIONS", "TRACE", "QUERY":
		return true
	}
	return false
}
