package yiigo

import (
	"path/filepath"
	"sync"
)

type cfgdb struct {
	name string
	dsn  string
}

type cfgmongo struct {
	name string
	dsn  string
}

type cfgredis struct {
	name    string
	address string
	options []RedisOption
}

type cfgnsq struct {
	nsqd    string
	lookupd []string
	options []NSQOption
}

type cfglogger struct {
	name    string
	path    string
	options []LoggerOption
}

type initSetting struct {
	logger []*cfglogger
	db     []*cfgdb
	mongo  []*cfgmongo
	redis  []*cfgredis
	nsq    *cfgnsq
}

// InitOption configures how we set up the yiigo initialization.
type InitOption func(s *initSetting)

// WithMongo register mongodb.
// [DSN] mongodb://localhost:27017/?connectTimeoutMS=10000&minPoolSize=10&maxPoolSize=20&maxIdleTimeMS=60000&readPreference=primary
// [reference] https://docs.mongodb.com/manual/reference/connection-string
func WithMongo(name string, dsn string) InitOption {
	return func(s *initSetting) {
		s.mongo = append(s.mongo, &cfgmongo{
			name: name,
			dsn:  dsn,
		})
	}
}

// WithRedis register redis.
func WithRedis(name, address string, options ...RedisOption) InitOption {
	return func(s *initSetting) {
		s.redis = append(s.redis, &cfgredis{
			name:    name,
			address: address,
			options: options,
		})
	}
}

// WithNSQ initialize the nsq.
func WithNSQ(nsqd string, lookupd []string, options ...NSQOption) InitOption {
	return func(s *initSetting) {
		s.nsq = &cfgnsq{
			nsqd:    nsqd,
			lookupd: lookupd,
			options: options,
		}
	}
}

// WithLogger register logger.
func WithLogger(name, logfile string, options ...LoggerOption) InitOption {
	return func(s *initSetting) {
		s.logger = append(s.logger, &cfglogger{
			name:    name,
			path:    filepath.Clean(logfile),
			options: options,
		})
	}
}

// Init yiigo initialization.
func Init(options ...InitOption) {
	setting := new(initSetting)

	for _, f := range options {
		f(setting)
	}

	if len(setting.logger) != 0 {
		for _, v := range setting.logger {
			initLogger(v.name, v.path, v.options...)
		}
	}

	var wg sync.WaitGroup

	if len(setting.db) != 0 {
		wg.Add(1)

	}

	if len(setting.mongo) != 0 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for _, v := range setting.mongo {
				initMongoDB(v.name, v.dsn)
			}
		}()
	}

	if len(setting.redis) != 0 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for _, v := range setting.redis {
				initRedis(v.name, v.address, v.options...)
			}
		}()
	}

	if setting.nsq != nil {
		wg.Add(1)

		go func() {
			defer wg.Done()

			initNSQ(setting.nsq.nsqd, setting.nsq.lookupd, setting.nsq.options...)
		}()
	}

	wg.Wait()
}
