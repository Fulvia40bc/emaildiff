package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

type normOptions struct {
	NoCase    bool
	StripPlus bool
	Domain    bool
}

type side struct {
	items []string
	seen  map[string]bool
	dups  map[string]bool
}

func newSide() *side {
	return &side{seen: map[string]bool{}, dups: map[string]bool{}}
}

func (s *side) reset() {
	s.items = nil
	s.seen = map[string]bool{}
	s.dups = map[string]bool{}
}

func normKey(e string, o normOptions) string {
	s := e
	if o.NoCase {
		s = strings.ToLower(s)
	}
	if o.StripPlus {
		if at := strings.LastIndex(s, "@"); at > 0 {
			if i := strings.Index(s[:at], "+"); i >= 0 {
				s = s[:i] + s[at:]
			}
		}
	}
	if o.Domain {
		if at := strings.LastIndex(s, "@"); at >= 0 {
			s = s[at:]
		}
	}
	return s
}

func extractEmails(text string) []string {
	return emailRe.FindAllString(text, -1)
}

func mergeEmails(s *side, found []string, o normOptions) int {
	added := 0
	for _, e := range found {
		k := normKey(e, o)
		if !s.seen[k] {
			s.seen[k] = true
			s.items = append(s.items, e)
			added++
		} else {
			s.dups[k] = true
		}
	}
	return added
}

type diffResult struct {
	onlyInA []string
	onlyInB []string
	common  int
}

func computeDiff(a, b *side, o normOptions) diffResult {
	var r diffResult
	for _, e := range a.items {
		if b.seen[normKey(e, o)] {
			r.common++
		} else {
			r.onlyInA = append(r.onlyInA, e)
		}
	}
	for _, e := range b.items {
		if !a.seen[normKey(e, o)] {
			r.onlyInB = append(r.onlyInB, e)
		}
	}
	return r
}

func readSharedStrings(f *zip.File) ([]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	var out []string
	inSi, inT := false, false
	var cur strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "si" {
				inSi = true
				cur.Reset()
			}
			if t.Name.Local == "t" && inSi {
				inT = true
			}
		case xml.CharData:
			if inT {
				cur.Write([]byte(t))
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inT = false
			}
			if t.Name.Local == "si" && inSi {
				out = append(out, cur.String())
				inSi = false
			}
		}
	}
	return out, nil
}

func readWorksheetCells(f *zip.File, shared []string) ([]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	var cells []string
	cellT := ""
	inV, inT := false, false
	var v, is strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "c":
				cellT = ""
				for _, a := range t.Attr {
					if a.Name.Local == "t" {
						cellT = a.Value
					}
				}
				v.Reset()
				is.Reset()
			case "v":
				inV = true
			case "t":
				inT = true
			}
		case xml.CharData:
			if inV {
				v.Write([]byte(t))
			}
			if inT {
				is.Write([]byte(t))
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "c":
				val := ""
				if cellT == "s" {
					if idx, err := strconv.Atoi(strings.TrimSpace(v.String())); err == nil && idx >= 0 && idx < len(shared) {
						val = shared[idx]
					}
				} else {
					val = v.String() + is.String()
				}
				cells = append(cells, val)
			}
		}
	}
	return cells, nil
}

func xlsxEmails(path string) ([]string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%s is not a readable .xlsx (zip) file: %w", filepath.Base(path), err)
	}
	defer zr.Close()
	var shared []string
	var sheets []*zip.File
	for _, f := range zr.File {
		switch {
		case f.Name == "xl/sharedStrings.xml":
			s, err := readSharedStrings(f)
			if err != nil {
				return nil, err
			}
			shared = s
		case strings.HasPrefix(f.Name, "xl/worksheets/") && strings.HasSuffix(f.Name, ".xml"):
			sheets = append(sheets, f)
		}
	}
	var all []string
	for _, f := range sheets {
		cells, err := readWorksheetCells(f, shared)
		if err != nil {
			return nil, err
		}
		for _, c := range cells {
			all = append(all, extractEmails(c)...)
		}
	}
	return all, nil
}

func loadFileEmails(path string) ([]string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".xlsx", ".xlsm":
		return xlsxEmails(path)
	case ".xls":
		return nil, fmt.Errorf("%s: legacy .xls is not supported, please save as .xlsx", filepath.Base(path))
	default:
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("could not read %s: %w", filepath.Base(path), err)
		}
		return extractEmails(string(data)), nil
	}
}

func encodeSession(a, b *side) string {
	var sb strings.Builder
	sb.WriteString("[A]\r\n")
	for _, e := range a.items {
		sb.WriteString(e + "\r\n")
	}
	sb.WriteString("[B]\r\n")
	for _, e := range b.items {
		sb.WriteString(e + "\r\n")
	}
	return sb.String()
}

func decodeSession(text string) ([]string, []string, error) {
	var a, b []string
	cur := -1
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch strings.ToLower(line) {
		case "[a]":
			cur = 0
			continue
		case "[b]":
			cur = 1
			continue
		}
		if cur == -1 {
			return nil, nil, fmt.Errorf("line outside [A]/[B] section: %q", line)
		}
		if emailRe.MatchString(line) {
			if cur == 0 {
				a = append(a, line)
			} else {
				b = append(b, line)
			}
		}
	}
	return a, b, nil
}
