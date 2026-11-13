package config

import "time"

// Config holds every setting. Each section is a group in the settings page, so
// keep related settings together and the sections short.
type Config struct {
	Data     Data     `key:"data"`
	Web      Web      `key:"web"`
	Log      Log      `key:"log"`
	Store    Store    `key:"store"`
	Crawler  Crawler  `key:"crawler"`
	DHT      DHT      `key:"dht"`
	Torznab  Torznab  `key:"torznab"`
	TMDB     TMDB     `key:"tmdb"`
	Classify Classify `key:"classify"`
}

// Data is where the product keeps its files. The directory is not stored in the
// settings database: the database is found through it.
type Data struct {
	Directory string `key:"directory" label:"Data Directory" persist:"false" usage:"Directory holding the databases, the shard files and the blocklist."`
}

// Web is the web interface and the Torznab endpoint.
type Web struct {
	Address string `key:"address" label:"Listen Address" usage:"The address the web interface and the Torznab endpoint listen on. Changing it needs a restart."`
	// SessionDays is how long a sign-in lasts. Existing cookies keep the expiry
	// they were issued with, so shortening it does not sign anyone out now.
	SessionDays       int    `key:"session_days" label:"Session Days" usage:"How long a sign-in lasts. Existing sessions keep the expiry they were issued with." min:"1" max:"365" advanced:"true"`
	AdminUsername     string `key:"admin_username" hidden:"true" usage:"Username of the administrator account. Set during setup." restart:"true"`
	AdminPasswordHash string `key:"admin_password_hash" hidden:"true" secret:"true" usage:"Password hash of the administrator account. Set during setup; reset it with bittrawler auth reset." restart:"true"`
	SessionSecret     string `key:"session_secret" hidden:"true" secret:"true" usage:"Key that signs login sessions. Generated on first start; rotating it signs everyone out." restart:"true"`
}

// Log configures the process log.
type Log struct {
	Level string `key:"level" usage:"Log level: debug, info, warn or error." enum:"debug,info,warn,error"`
}

// Store configures the torrent store.
type Store struct {
	RetentionMonths int `key:"retention_months" label:"Retention Months"  usage:"How many calendar months of torrents to keep. Zero keeps everything." min:"0"`
}

// Crawler configures discovery, metadata fetching and writing.
type Crawler struct {
	Enabled                     bool          `key:"enabled" label:"Crawling Enabled" usage:"Crawl whenever the process is running. The web interface can pause and resume it."`
	BootstrapNodes              []string      `key:"bootstrap_nodes" usage:"host:port pairs used to seed the routing table. They move over time; the list is a hint."`
	OldNodeAge                  time.Duration `key:"old_node_age" label:"Node Timeout"  usage:"How long a node may go without answering before it is pinged again."`
	OldNodeCheckInterval        time.Duration `key:"old_node_check_interval" usage:"How often the routing table is scanned for stale nodes." advanced:"true"`
	BootstrapReseedInterval     time.Duration `key:"bootstrap_reseed_interval" usage:"How often the bootstrap nodes are resolved again." advanced:"true"`
	SampleInfoHashesOverride    time.Duration `key:"sample_infohashes_override" label:"Resample Interval"  usage:"Re-sample a node after this long when its reply contained unseen hashes. Zero honours the interval the node asks for."`
	SeenHashesSize              int           `key:"seen_hashes_size" usage:"How many infohashes the in-process seen filter is sized for. It stops the same hash being triaged twice." min:"1" advanced:"true"`
	SeenHashesFalsePositiveRate float64       `key:"seen_hashes_false_positive_rate" label:"Seen Filter Error Rate"  usage:"False positive rate of the seen filter. A false positive loses a new torrent, so keep it low." advanced:"true"`
	TriageBatchSize             int           `key:"triage_batch_size" label:"Triage Batch"  usage:"How many discovered hashes are checked against the blocklist and the store at once." min:"1" advanced:"true"`
	TriageBatchWait             time.Duration `key:"triage_batch_wait" label:"Triage Batch Wait"  usage:"How long discovered hashes may wait for a batch to fill." advanced:"true"`
	PersistBatchSize            int           `key:"persist_batch_size" label:"Write Batch"  usage:"How many crawled torrents are written in one transaction." min:"1" advanced:"true"`
	PersistBatchWait            time.Duration `key:"persist_batch_wait" label:"Write Batch Wait"  usage:"How long crawled torrents may wait for a write batch to fill." advanced:"true"`
	PeersPerInfoHash            int           `key:"peers_per_infohash" usage:"How many peers metadata is requested from before giving up on a torrent." min:"1" advanced:"true"`
	MetadataTimeout             time.Duration `key:"metadata_timeout" usage:"Whole budget for fetching one torrent's metadata, across every peer tried."`
	Workers                     Workers       `key:"workers"`
}

// Workers sets how many goroutines run each crawler stage.
type Workers struct {
	Ping             int `key:"ping" label:"Ping Workers" usage:"Goroutines that check whether stale nodes are still alive." min:"1" advanced:"true"`
	FindNode         int `key:"find_node" label:"Find Node Workers" usage:"Goroutines that grow the routing table by asking for nodes near a random target." min:"1" advanced:"true"`
	SampleInfoHashes int `key:"sample_infohashes" label:"Sample Workers" usage:"Goroutines that ask BEP 51 capable nodes for infohashes." min:"1" advanced:"true"`
	GetPeers         int `key:"get_peers" label:"Get Peers Workers" usage:"Goroutines that look up peers for an unseen hash." min:"1" advanced:"true"`
	Metadata         int `key:"metadata" label:"Metadata Workers" usage:"Goroutines that fetch metadata from peers." min:"1" advanced:"true"`
}

// DHT configures the UDP socket and the replies to other nodes.
type DHT struct {
	ListenAddress            string        `key:"listen_address" usage:"Address for the DHT UDP socket, as host:port. Changing it needs a restart."`
	QueryTimeout             time.Duration `key:"query_timeout" usage:"How long an outgoing DHT query waits for a response."`
	ResponderTimeout         time.Duration `key:"responder_timeout" usage:"How long an answer to an incoming query may take before it is dropped." advanced:"true"`
	SampleInfoHashesInterval int           `key:"sample_infohashes_interval" label:"Sample Interval"  usage:"BEP 51 interval, in seconds, advertised to other nodes." min:"1" advanced:"true"`
	OverallQueryRate         float64       `key:"overall_query_rate" usage:"Incoming queries answered per second, across all sources." advanced:"true"`
	OverallQueryBurst        int           `key:"overall_query_burst" usage:"Burst size for the overall incoming query limit." min:"1" advanced:"true"`
	PerIPQueryRate           float64       `key:"per_ip_query_rate" label:"Per-IP Query Rate"  usage:"Incoming queries answered per second, per source address." advanced:"true"`
	PerIPQueryBurst          int           `key:"per_ip_query_burst" label:"Per-IP Query Burst"  usage:"Burst size for the per address incoming query limit." min:"1" advanced:"true"`
	OutboundRate             float64       `key:"outbound_rate" usage:"Outgoing queries sent per second, per remote address." advanced:"true"`
	OutboundBurst            int           `key:"outbound_burst" usage:"Burst size for the outgoing query limit." min:"1" advanced:"true"`
}

// Torznab configures the indexer endpoint the Arr applications talk to.
type Torznab struct {
	APIKey       string `key:"api_key" hidden:"true" secret:"true" usage:"API key Prowlarr sends. Generated on first start; view and rotate it on the settings page."`
	Title        string `key:"title" usage:"Title reported to clients in the capabilities response."`
	DefaultLimit int    `key:"default_limit" label:"Default Page Size"  usage:"Results per page when the request does not ask for a size." min:"1" advanced:"true"`
	MaxLimit     int    `key:"max_limit" label:"Maximum Page Size"  usage:"Largest page the endpoint will serve." min:"1" advanced:"true"`
}

// TMDB configures the classification confirmation stage. It is disabled while
// no token is set.
type TMDB struct {
	Token     string        `key:"token" secret:"true" usage:"TMDB v4 read access token. Empty disables confirmation."`
	Workers   int           `key:"workers" usage:"Goroutines that confirm classifications against TMDB." min:"1" advanced:"true"`
	Timeout   time.Duration `key:"timeout" usage:"Budget for one TMDB lookup including its HTTP requests." advanced:"true"`
	RateLimit float64       `key:"rate_limit" usage:"TMDB requests per second." advanced:"true"`
}

// Classify configures what is dropped at intake instead of being indexed.
type Classify struct {
	DropAdult   bool     `key:"drop_adult" label:"Drop Adult Torrents"  usage:"Drop adult torrents instead of indexing them as XXX."`
	DropScripts []string `key:"drop_scripts" usage:"Writing systems to drop: cjk, hangul, cyrillic, arabic, hebrew, thai, devanagari, greek, nonlatin. Empty keeps everything."`
}

// Default returns the settings new installations start with.
func Default() Config {
	return Config{
		Data: Data{
			Directory: "./data",
		},
		Web: Web{
			Address:     "0.0.0.0:6684",
			SessionDays: 30,
		},
		Log: Log{
			Level: "info",
		},
		Store: Store{
			RetentionMonths: 12,
		},
		Crawler: Crawler{
			Enabled:                     true,
			BootstrapNodes:              defaultBootstrapNodes(),
			OldNodeAge:                  time.Minute * 15,
			OldNodeCheckInterval:        time.Second * 10,
			BootstrapReseedInterval:     time.Minute * 10,
			SampleInfoHashesOverride:    time.Minute,
			SeenHashesSize:              10_000_000,
			SeenHashesFalsePositiveRate: 0.001,
			TriageBatchSize:             500,
			TriageBatchWait:             time.Second * 5,
			PersistBatchSize:            100,
			PersistBatchWait:            time.Second * 5,
			PeersPerInfoHash:            4,
			MetadataTimeout:             time.Second * 45,
			Workers: Workers{
				Ping:             4,
				FindNode:         8,
				SampleInfoHashes: 8,
				GetPeers:         16,
				Metadata:         32,
			},
		},
		DHT: DHT{
			ListenAddress:            "0.0.0.0:6684",
			QueryTimeout:             time.Second * 4,
			ResponderTimeout:         time.Second * 5,
			SampleInfoHashesInterval: 10,
			OverallQueryRate:         50,
			OverallQueryBurst:        20,
			PerIPQueryRate:           1,
			PerIPQueryBurst:          10,
			OutboundRate:             1,
			OutboundBurst:            4,
		},
		Torznab: Torznab{
			Title:        "BitTrawler",
			DefaultLimit: 100,
			MaxLimit:     100,
		},
		TMDB: TMDB{
			Workers:   4,
			Timeout:   time.Second * 5,
			RateLimit: 4,
		},
	}
}

// defaultBootstrapNodes is the set the reference DHT clients use. They are
// addresses of long-running routers, not nodes we control, so they are only a
// starting point for the routing table.
func defaultBootstrapNodes() []string {
	return []string{
		"router.utorrent.com:6881",
		"router.bittorrent.com:6881",
		"dht.transmissionbt.com:6881",
		"dht.aelitis.com:6881",
		"router.silotis.us:6881",
		"dht.libtorrent.org:25401",
	}
}
