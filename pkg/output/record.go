package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type RecordType string

const (
	TypeScanStart RecordType = "scan_start"
	TypeService   RecordType = "service"
	TypeWeb       RecordType = "web"
	TypeLoot      RecordType = "loot"
	TypeScanEnd   RecordType = "scan_end"
)

type Record struct {
	Type      RecordType      `json:"type"`
	Timestamp time.Time       `json:"ts"`
	Data      json.RawMessage `json:"data"`
}

func NewRecord(t RecordType, data interface{}) Record {
	raw, _ := json.Marshal(data)
	return Record{
		Type:      t,
		Timestamp: time.Now(),
		Data:      raw,
	}
}

func (r Record) Marshal() []byte {
	b, _ := json.Marshal(r)
	return b
}

func ParseRecord(line []byte) (Record, error) {
	var r Record
	err := json.Unmarshal(line, &r)
	return r, err
}

func ParseRecordData[T any](r Record) (T, error) {
	var v T
	err := json.Unmarshal(r.Data, &v)
	return v, err
}

func ParseRecordFile(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		r, err := ParseRecord(line)
		if err != nil {
			continue
		}
		records = append(records, r)
	}
	return records, scanner.Err()
}

func RenderFile(path, format, outputPath string) error {
	records, err := ParseRecordFile(path)
	if err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if outputPath != "" {
		outFile, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer outFile.Close()
		w = outFile
	}

	switch strings.ToLower(format) {
	case "markdown", "md":
		return RenderRecordsMarkdown(w, records)
	default:
		return RenderRecordsTerminal(w, records)
	}
}
