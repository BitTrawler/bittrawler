package banning

import (
	"errors"

	"github.com/BitTrawler/bittrawler/internal/protocol/metainfo"
)

// Checker rejects metainfo that is not worth indexing.
type Checker interface {
	Check(metainfo.Info) error
}

// New combines the default checks with any extra ones. All failures are
// returned together so a log line says everything that was wrong.
func New(extra ...Checker) Checker {
	checkers := make([]Checker, 0, len(extra)+3)
	checkers = append(checkers, extra...)
	checkers = append(
		checkers,
		nameLengthChecker{min: 8},
		sizeChecker{min: 1024},
		utf8Checker{},
	)

	return combinedChecker{checkers: checkers}
}

type combinedChecker struct {
	checkers []Checker
}

func (c combinedChecker) Check(info metainfo.Info) error {
	errs := make([]error, 0, len(c.checkers))

	for _, checker := range c.checkers {
		if err := checker.Check(info); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
