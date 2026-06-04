package trajectory

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
)

func ReadFile(path string) ([]Event, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Read(file)
}

func Read(r io.Reader) ([]Event, error) {
	reader := bufio.NewReader(r)
	var events []Event
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var event Event
			if err := json.Unmarshal(trimNewline(line), &event); err != nil {
				return nil, err
			}
			events = append(events, event)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return events, nil
}

func trimNewline(data []byte) []byte {
	for len(data) > 0 && (data[len(data)-1] == '\n' || data[len(data)-1] == '\r') {
		data = data[:len(data)-1]
	}
	return data
}
