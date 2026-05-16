package notification

import "time"

// discordTestBackoffOverride shortens the 5xx retry backoff for tests and
// returns a function that restores the original value.
func discordTestBackoffOverride() func() {
	prev := discordServerErrorBackoff
	discordServerErrorBackoff = 10 * time.Millisecond
	return func() { discordServerErrorBackoff = prev }
}
