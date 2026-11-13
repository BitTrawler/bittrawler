// Package lexer provides a rune-oriented reader for the small grammars used to
// parse search queries and keyword expressions.
package lexer

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"unicode"
)

func NewLexer(str string) Lexer {
	return Lexer{
		reader: bufio.NewReader(strings.NewReader(str)),
	}
}

type Lexer struct {
	pos    int
	reader *bufio.Reader
}

func (l *Lexer) Pos() int {
	return l.pos
}

func (l *Lexer) Read() (rune, bool) {
	r, _, err := l.reader.ReadRune()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, false
		}

		panic(err)
	}

	l.pos++

	return r, true
}

func (l *Lexer) Backup() {
	if err := l.reader.UnreadRune(); err != nil {
		panic(err)
	}

	l.pos--
}

func (l *Lexer) ReadIf(fn func(rune) bool) (rune, bool) {
	r, ok := l.Read()
	if !ok {
		return 0, false
	}

	if !fn(r) {
		l.Backup()
		return 0, false
	}

	return r, true
}

func (l *Lexer) ReadChar(r1 rune) bool {
	_, ok := l.ReadIf(IsChar(r1))
	return ok
}

func IsChar(r1 rune) func(rune) bool {
	return func(r2 rune) bool {
		return r1 == r2
	}
}

func IsWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
