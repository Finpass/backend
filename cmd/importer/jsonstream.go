package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// streamArray decodes a top-level JSON array one element at a time.
func streamArray[T any](path string, fn func(T) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	if t, err := dec.Token(); err != nil || t != json.Delim('[') {
		return fmt.Errorf("%s: expected top-level array (%v)", path, err)
	}
	for dec.More() {
		var v T
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := fn(v); err != nil {
			return err
		}
	}
	return nil
}

// streamObject walks a top-level JSON object, handing each key's value to fn through the decoder.
func streamObject(path string, fn func(key string, dec *json.Decoder) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return fmt.Errorf("%s: expected top-level object (%v)", path, err)
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return err
		}
		if err := fn(k.(string), dec); err != nil {
			return err
		}
	}
	return nil
}
