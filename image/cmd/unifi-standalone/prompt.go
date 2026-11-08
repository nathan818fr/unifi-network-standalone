package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func askLine(out io.Writer, question string) (string, error) {
	tty, err := openTerminal()
	if err != nil {
		return "", err
	}
	defer func() { _ = tty.Close() }()
	if _, err := fmt.Fprint(out, question); err != nil {
		return "", err
	}
	return readLine(tty)
}

// askHidden asks for a secret, with the terminal's echo off. It uses stty rather than a terminal
// package, for a single prompt.
func askHidden(out io.Writer, question string) (string, error) {
	tty, err := openTerminal()
	if err != nil {
		return "", err
	}
	defer func() { _ = tty.Close() }()
	if _, err := fmt.Fprint(out, question); err != nil {
		return "", err
	}
	if err := stty(tty, "-echo"); err != nil {
		return "", err
	}
	answer, err := readLine(tty)
	if restoreErr := stty(tty, "echo"); err == nil {
		err = restoreErr
	}
	if _, printErr := fmt.Fprintln(out); err == nil {
		err = printErr
	}
	return answer, err
}

func openTerminal() (*os.File, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, errors.New("no terminal to ask from")
	}
	return tty, nil
}

func readLine(in io.Reader) (string, error) {
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading the answer: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func stty(tty *os.File, setting string) error {
	command := exec.Command("stty", setting)
	command.Stdin = tty
	if err := command.Run(); err != nil {
		return fmt.Errorf("stty %s: %w", setting, err)
	}
	return nil
}
