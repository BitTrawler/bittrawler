package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Validate checks the settings against the rules that cannot be expressed on a
// single field.
func (c Config) Validate() error {
	var errs []error

	for _, e := range builtEntries() {
		if err := e.check(&c); err != nil {
			errs = append(errs, fmt.Errorf("config: %w", err))
		}
	}

	if strings.TrimSpace(c.Data.Directory) == "" {
		errs = append(errs, errors.New("config: data.directory is required"))
	}

	if err := checkListenAddress(c.Web.Address); err != nil {
		errs = append(errs, fmt.Errorf("config: web.address: %w", err))
	}

	if _, err := netip.ParseAddrPort(c.DHT.ListenAddress); err != nil {
		errs = append(errs, fmt.Errorf("config: dht.listen_address: %w", err))
	}

	errs = append(errs, validateAdmin(c.Web)...)

	if c.Torznab.DefaultLimit > c.Torznab.MaxLimit {
		errs = append(errs, errors.New("config: torznab.default_limit must not exceed torznab.max_limit"))
	}

	positive := []struct {
		key   string
		value time.Duration
	}{
		{"crawler.old_node_age", c.Crawler.OldNodeAge},
		{"crawler.old_node_check_interval", c.Crawler.OldNodeCheckInterval},
		{"crawler.bootstrap_reseed_interval", c.Crawler.BootstrapReseedInterval},
		{"crawler.triage_batch_wait", c.Crawler.TriageBatchWait},
		{"crawler.persist_batch_wait", c.Crawler.PersistBatchWait},
		{"crawler.metadata_timeout", c.Crawler.MetadataTimeout},
		{"dht.query_timeout", c.DHT.QueryTimeout},
		{"dht.responder_timeout", c.DHT.ResponderTimeout},
		{"tmdb.timeout", c.TMDB.Timeout},
	}

	for _, setting := range positive {
		if setting.value <= 0 {
			errs = append(errs, fmt.Errorf("config: %s must be positive", setting.key))
		}
	}

	if c.Crawler.SampleInfoHashesOverride < 0 {
		errs = append(errs, errors.New("config: crawler.sample_infohashes_override must not be negative"))
	}

	// The feed polls this often while a page is open; a shorter interval would
	// be a request loop against the instance itself.
	if c.Overview.Refresh < time.Second {
		errs = append(errs, errors.New("config: overview.refresh must be at least 1s"))
	}

	if c.Crawler.SeenHashesFalsePositiveRate <= 0 || c.Crawler.SeenHashesFalsePositiveRate >= 1 {
		errs = append(errs, errors.New("config: crawler.seen_hashes_false_positive_rate must be between 0 and 1"))
	}

	rates := []struct {
		key   string
		value float64
	}{
		{"dht.overall_query_rate", c.DHT.OverallQueryRate},
		{"dht.per_ip_query_rate", c.DHT.PerIPQueryRate},
		{"dht.outbound_rate", c.DHT.OutboundRate},
		{"tmdb.rate_limit", c.TMDB.RateLimit},
	}

	for _, rate := range rates {
		if rate.value <= 0 {
			errs = append(errs, fmt.Errorf("config: %s must be positive", rate.key))
		}
	}

	errs = append(errs, validateBootstrapNodes(c.Crawler.BootstrapNodes)...)
	errs = append(errs, validateDropScripts(c.Classify.DropScripts)...)
	errs = append(errs, validateRejectedReasons(c.Overview.RejectedReasons)...)

	return errors.Join(errs...)
}

// validateAdmin checks that the administrator account is either completely
// unset, which means setup has not run, or complete.
func validateAdmin(web Web) []error {
	username := strings.TrimSpace(web.AdminUsername)
	hash := strings.TrimSpace(web.AdminPasswordHash)

	switch {
	case username == "" && hash == "":
		return nil
	case username == "":
		return []error{errors.New("config: web.admin_username is required when web.admin_password_hash is set")}
	case hash == "":
		return []error{errors.New("config: web.admin_password_hash is required when web.admin_username is set")}
	default:
		return nil
	}
}

// checkListenAddress accepts an address that can be handed to net.Listen: a
// host or empty host, and a port.
func checkListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}

	if host != "" {
		if _, err := netip.ParseAddr(host); err != nil {
			return fmt.Errorf("host %q is not an IP address", host)
		}
	}

	return checkPort(port)
}

func checkPort(port string) error {
	number, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("port %q is not a number", port)
	}

	// Port zero asks the operating system for a free port, which is useful in
	// tests and for a second instance on one machine.
	if number < 0 || number > 65535 {
		return fmt.Errorf("port %d is out of range", number)
	}

	return nil
}

// validateBootstrapNodes checks that every entry is a host:port pair. The host
// is resolved at startup rather than now, because the addresses move.
func validateBootstrapNodes(nodes []string) []error {
	var errs []error

	for i, node := range nodes {
		host, port, err := net.SplitHostPort(node)
		if err != nil {
			errs = append(errs, fmt.Errorf("config: crawler.bootstrap_nodes[%d]: invalid host:port %q", i, node))

			continue
		}

		if host == "" {
			errs = append(errs, fmt.Errorf("config: crawler.bootstrap_nodes[%d]: %q has no host", i, node))

			continue
		}

		if err := checkPort(port); err != nil {
			errs = append(errs, fmt.Errorf("config: crawler.bootstrap_nodes[%d]: %w", i, err))
		}
	}

	return errs
}

// dropScripts are the names accepted by classify.Config.DropScripts. Keeping
// the list here means a bad value is caught when the settings are loaded
// instead of silently doing nothing.
var dropScripts = []string{
	"cjk", "hangul", "cyrillic", "arabic", "hebrew",
	"thai", "devanagari", "greek", "nonlatin",
}

func validateDropScripts(names []string) []error {
	var errs []error

	for _, name := range names {
		if !slices.Contains(dropScripts, name) {
			errs = append(errs, fmt.Errorf("config: classify.drop_scripts: unknown writing system %q", name))
		}
	}

	return errs
}

// rejectedReasons are the reason names the overview's rejected feed accepts.
// Keeping the list here means a bad value is caught when the settings are
// loaded instead of silently hiding every rejection.
var rejectedReasons = []string{"banned", "deceptive", "adult", "script", "blocked"}

func validateRejectedReasons(names []string) []error {
	var errs []error

	for _, name := range names {
		if !slices.Contains(rejectedReasons, name) {
			errs = append(errs, fmt.Errorf("config: overview.rejected_reasons: unknown reason %q", name))
		}
	}

	return errs
}
