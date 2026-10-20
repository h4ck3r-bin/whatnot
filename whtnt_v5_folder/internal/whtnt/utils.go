package whtnt

import (
	"bufio"
	"errors"
	"os"
	"strings"
	"time"
)

func loadTextLines(fileName string) ([]string, error) {
	file, err := os.Open(fileName)

	if err != nil {
		return nil, err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)

	var lines []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

func marshalOrdered(kvPairs ...string) ([]byte, error) {
	if len(kvPairs)%2 != 0 {
		return nil, errors.New("marshalOrdered requires an even number of arguments")
	}
	var buf []byte
	buf = append(buf, '{')
	for i := 0; i < len(kvPairs); i += 2 {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '"')
		buf = append(buf, kvPairs[i]...)
		buf = append(buf, '"', ':', '"')
		for _, c := range []byte(kvPairs[i+1]) {
			switch c {
			case '"':
				buf = append(buf, '\\', '"')
			case '\\':
				buf = append(buf, '\\', '\\')
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			default:
				buf = append(buf, c)
			}
		}
		buf = append(buf, '"')
	}
	buf = append(buf, '}')
	return buf, nil
}

func replaceFileWithRetry(tempName string, path string) error {
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(100+attempt*50) * time.Millisecond)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			lastErr = err
			continue
		}
		if err := os.Rename(tempName, path); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}
