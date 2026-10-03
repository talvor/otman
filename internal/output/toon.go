package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// EncodeTOON writes v in TOON (Token-Oriented Object Notation), the compact
// AXI format. It covers the subset otman's fixed shapes need: objects, arrays
// of primitives (inline), arrays of uniform flat objects (tabular) and other
// arrays (list items). The value goes through encoding/json first, so JSON
// struct tags decide field names, order and omission in every format alike.
func EncodeTOON(w io.Writer, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tree, err := decodeOrdered(dec)
	if err != nil {
		return err
	}
	var b strings.Builder
	switch t := tree.(type) {
	case object:
		writeObject(&b, t, 0)
	case []any:
		writeField(&b, "", t, 0)
	default:
		b.WriteString(scalar(t))
		b.WriteByte('\n')
	}
	_, err = io.WriteString(w, b.String())
	return err
}

type member struct {
	key string
	val any
}

// object is a JSON object with its key order preserved.
type object []member

func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		obj := object{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			obj = append(obj, member{k.(string), v})
		}
		_, err := dec.Token()
		return obj, err
	case json.Delim('['):
		arr := []any{}
		for dec.More() {
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		_, err := dec.Token()
		return arr, err
	}
	return tok, nil
}

func writeObject(b *strings.Builder, obj object, depth int) {
	for _, m := range obj {
		writeField(b, key(m.key), m.val, depth)
	}
}

func writeField(b *strings.Builder, k string, v any, depth int) {
	ind := strings.Repeat("  ", depth)
	switch t := v.(type) {
	case object:
		fmt.Fprintf(b, "%s%s:\n", ind, k)
		writeObject(b, t, depth+1)
	case []any:
		writeArray(b, k, t, depth)
	default:
		fmt.Fprintf(b, "%s%s: %s\n", ind, k, scalar(t))
	}
}

func writeArray(b *strings.Builder, k string, arr []any, depth int) {
	ind := strings.Repeat("  ", depth)
	if allScalar(arr) {
		cells := make([]string, len(arr))
		for i, v := range arr {
			cells[i] = scalar(v)
		}
		sep := " "
		if len(arr) == 0 {
			sep = ""
		}
		fmt.Fprintf(b, "%s%s[%d]:%s%s\n", ind, k, len(arr), sep, strings.Join(cells, ","))
		return
	}
	if fields, ok := tabular(arr); ok {
		keys := make([]string, len(fields))
		for i, f := range fields {
			keys[i] = key(f)
		}
		fmt.Fprintf(b, "%s%s[%d]{%s}:\n", ind, k, len(arr), strings.Join(keys, ","))
		for _, row := range arr {
			cells := make([]string, len(fields))
			for i, m := range row.(object) {
				cells[i] = scalar(m.val)
			}
			fmt.Fprintf(b, "%s  %s\n", ind, strings.Join(cells, ","))
		}
		return
	}
	fmt.Fprintf(b, "%s%s[%d]:\n", ind, k, len(arr))
	for _, item := range arr {
		switch t := item.(type) {
		case object:
			if len(t) == 0 {
				fmt.Fprintf(b, "%s  -\n", ind)
				continue
			}
			// The first field sits on the dash line; the rest align under it.
			var first strings.Builder
			writeField(&first, key(t[0].key), t[0].val, depth+2)
			fmt.Fprintf(b, "%s  - %s", ind, strings.TrimLeft(first.String(), " "))
			writeObject(b, t[1:], depth+2)
		case []any:
			var nested strings.Builder
			writeArray(&nested, "", t, depth+2)
			fmt.Fprintf(b, "%s  - %s", ind, strings.TrimLeft(nested.String(), " "))
		default:
			fmt.Fprintf(b, "%s  - %s\n", ind, scalar(t))
		}
	}
}

func allScalar(arr []any) bool {
	for _, v := range arr {
		switch v.(type) {
		case object, []any:
			return false
		}
	}
	return true
}

// tabular reports whether arr is non-empty and every element is an object
// with the same keys, in the same order, holding only scalars.
func tabular(arr []any) ([]string, bool) {
	if len(arr) == 0 {
		return nil, false
	}
	first, ok := arr[0].(object)
	if !ok || len(first) == 0 {
		return nil, false
	}
	fields := make([]string, len(first))
	for i, m := range first {
		fields[i] = m.key
	}
	for _, v := range arr {
		obj, ok := v.(object)
		if !ok || len(obj) != len(fields) {
			return nil, false
		}
		for i, m := range obj {
			if m.key != fields[i] {
				return nil, false
			}
			switch m.val.(type) {
			case object, []any:
				return nil, false
			}
		}
	}
	return fields, true
}

var (
	bareKey    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
	numberLike = regexp.MustCompile(`^-?\d+(\.\d+)?([eE][+-]?\d+)?$|^0\d+$`)
)

func key(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return quote(k)
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case json.Number:
		return t.String()
	case string:
		if needsQuote(t) {
			return quote(t)
		}
		return t
	}
	return fmt.Sprint(v)
}

func needsQuote(s string) bool {
	if s == "" || s != strings.TrimSpace(s) || s == "true" || s == "false" || s == "null" {
		return true
	}
	if numberLike.MatchString(s) || strings.HasPrefix(s, "-") {
		return true
	}
	return strings.ContainsAny(s, ",:\"\\[]{}\n\r\t")
}

func quote(s string) string {
	var b bytes.Buffer
	_ = newJSONEncoder(&b).Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
