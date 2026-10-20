package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxSetupInput = 4096

var errSetupCancelled = errors.New("setup canceled")

type setupPrompter struct {
	ctx context.Context
	in  io.Reader
	out io.Writer
}

func (p setupPrompter) ask(prompt, defaultValue string) (string, error) {
	if _, err := fmt.Fprint(p.out, prompt+": "); err != nil {
		return "", errors.New("could not write setup prompt")
	}
	value, err := p.read(func() (string, error) { return readSetupLine(p.in) })
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func (p setupPrompter) confirm(prompt string, defaultValue bool) (bool, error) {
	defaultAnswer := "n"
	if defaultValue {
		defaultAnswer = "y"
	}
	for {
		answer, err := p.ask(prompt+" (y/n)", defaultAnswer)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		if _, err := fmt.Fprintln(p.out, "Enter y or n."); err != nil {
			return false, errors.New("could not write setup prompt")
		}
	}
}

func (p setupPrompter) choose(prompt string, options []string, defaultIndex int) (int, error) {
	if err := validateSetupChoices(options, defaultIndex); err != nil {
		return 0, err
	}
	if err := p.printChoices(prompt, options); err != nil {
		return 0, err
	}
	for {
		answer, err := p.ask("Choose 1-"+strconv.Itoa(len(options)), strconv.Itoa(defaultIndex+1))
		if err != nil {
			return 0, err
		}
		selection, err := strconv.Atoi(answer)
		if err == nil && selection >= 1 && selection <= len(options) {
			return selection - 1, nil
		}
		if _, err := fmt.Fprintln(p.out, "Enter one of the listed numbers."); err != nil {
			return 0, errors.New("could not write setup prompt")
		}
	}
}

func validateSetupChoices(options []string, defaultIndex int) error {
	if len(options) == 0 || defaultIndex < 0 || defaultIndex >= len(options) {
		return errors.New("invalid setup choices")
	}
	return nil
}

func (p setupPrompter) printChoices(prompt string, options []string) error {
	if _, err := fmt.Fprintln(p.out, prompt); err != nil {
		return errors.New("could not write setup prompt")
	}
	for index, option := range options {
		if _, err := fmt.Fprintf(p.out, "  %d) %s\n", index+1, option); err != nil {
			return errors.New("could not write setup prompt")
		}
	}
	return nil
}

type setupInputResult struct {
	value string
	err   error
}

func (p setupPrompter) read(read func() (string, error)) (string, error) {
	if p.ctx == nil {
		return read()
	}
	if p.ctx.Err() != nil {
		return "", errSetupCancelled
	}
	result := make(chan setupInputResult, 1)
	go func() {
		value, err := read()
		result <- setupInputResult{value: value, err: err}
	}()
	select {
	case answer := <-result:
		if p.ctx.Err() != nil {
			return "", errSetupCancelled
		}
		return answer.value, answer.err
	case <-p.ctx.Done():
		return "", errSetupCancelled
	}
}

// Read byte by byte so a prompt never consumes input meant for the next one.
func readSetupLine(reader io.Reader) (string, error) {
	var content []byte
	var next [1]byte
	for {
		if _, err := io.ReadFull(reader, next[:]); err != nil {
			return "", setupReadError(err)
		}
		switch next[0] {
		case '\n':
			return completeSetupLine(content)
		case 3, 4:
			return "", errSetupCancelled
		}
		if len(content) > maxSetupInput {
			return "", errors.New("setup input exceeds the 4096 byte limit")
		}
		content = append(content, next[0])
	}
}

func completeSetupLine(content []byte) (string, error) {
	value := strings.TrimSuffix(string(content), "\r")
	if len(value) > maxSetupInput {
		return "", errors.New("setup input exceeds the 4096 byte limit")
	}
	return value, nil
}

func setupReadError(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return errSetupCancelled
	}
	return errors.New("could not read setup input")
}
