// Package donation holds the operator's per-algorithm donation targets.
//
// These are served to miners as "donate_config" in the /api/miners/submit
// response — a JSON object keyed by algorithm name. Each entry only stores the
// bare host:port; the stratum+tcp:// scheme is added automatically by FormatURL
// so the operator doesn't have to remember the URL scheme each algorithm
// expects. The C++ client parses the resulting
// mining_url/wallet/password/worker object (ConfigManager::ParseConfigFromJson)
// and falls back to the hardcoded copies in src/main.cpp (fallbackDonateConfig)
// when the panel cannot be reached. Keep both sources in sync.
package donation

// Config is the donation target for a single algorithm. Host is the bare
// host:port (no scheme). Donations are always plain TCP; pearlhash is
// special-cased in FormatURL and is sent as the bare host:port.
type Config struct {
	Host     string
	Wallet   string
	Password string
	Worker   string
}

// Configs is the hardcoded, per-algorithm donation configuration.
var Configs = map[string]Config{
	"progpowz": {
		Host:     "progpowz.donate.example.com:4444",
		Wallet:   "ProgpowDonateWallet",
		Password: "x",
		Worker:   "donate",
	},
	"kawpow": {
		Host:     "kawpow.donate.example.com:4444",
		Wallet:   "KawpowDonateWallet",
		Password: "x",
		Worker:   "donate",
	},
	"pearlhash": {
		Host:     "pearlhash.donate.example.com:4444",
		Wallet:   "PearlDonateWallet",
		Password: "x",
		Worker:   "donate",
	},
}

// FormatURL returns the full mining URL for an algorithm, applying the scheme
// WildRig expects. Donations always use plain TCP; pearlhash pools are served
// by address only (no scheme) and are returned unchanged.
func FormatURL(algo, host string) string {
	if algo == "pearlhash" {
		return host
	}
	return "stratum+tcp://" + host
}

// Map returns the donate_config payload keyed by algorithm, with each entry's
// mining_url already formatted for the algorithm.
func Map() map[string]map[string]string {
	out := make(map[string]map[string]string, len(Configs))
	for algo, cfg := range Configs {
		out[algo] = map[string]string{
			"mining_url": FormatURL(algo, cfg.Host),
			"wallet":     cfg.Wallet,
			"password":   cfg.Password,
			"worker":     cfg.Worker,
		}
	}
	return out
}
