package retry

import (
	"fmt"
	"time"
)

type Retryer struct {
	MaxAttempts int           `mapstructure:"max_attempts"`
	Sleep       time.Duration `mapstructure:"sleep"`
}

func (r Retryer) Do(f func() (error, bool)) error {
	return Retry(r.MaxAttempts, r.Sleep, f)
}

func Retry(attempts int, sleep time.Duration, f func() (error, bool)) error {
	if err, isRetryable := f(); err != nil {
		if !isRetryable {
			return err
		}

		if attempts = attempts - 1; attempts > 0 {
			fmt.Printf("Error: %s, Sleeping for %v before retrying\n", err.Error(), sleep)
			time.Sleep(sleep)
			fmt.Printf("Retrying with remaining attempts: %v\n", attempts)
			return Retry(attempts, sleep, f)
		}
		return err
	}

	return nil
}
