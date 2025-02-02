package yiigo

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

// EnvEventFunc the function that runs each time env change occurs.
type EnvEventFunc func(event fsnotify.Event)

type environment struct {
	path      string
	watcher   bool
	onchanges []EnvEventFunc
}

// EnvOption configures how we set up env file.
type EnvOption func(e *environment)

// WithEnvFile specifies the env file.
func WithEnvFile(filename string) EnvOption {
	return func(e *environment) {
		if len(strings.TrimSpace(filename)) == 0 {
			return
		}

		e.path = filepath.Clean(filename)
	}
}

// WithEnvWatcher watching and re-reading env file.
func WithEnvWatcher(fn ...EnvEventFunc) EnvOption {
	return func(e *environment) {
		e.watcher = true
		e.onchanges = append(e.onchanges, fn...)
	}
}

// LoadEnv will read your env file(s) and load them into ENV for this process.
// It will default to loading .env in the current path if not specifies the filename.
func LoadEnv(options ...EnvOption) error {
	env := &environment{path: ".env"}

	for _, f := range options {
		f(env)
	}

	abspath, err := filepath.Abs(env.path)

	if err != nil {
		return err
	}

	statEnvFile(abspath)

	if err := godotenv.Overload(abspath); err != nil {
		return err
	}

	if env.watcher {
		go watchEnvFile(abspath, env.onchanges...)
	}

	return nil
}

func statEnvFile(path string) {
	_, err := os.Stat(path)

	if err == nil {
		return
	}

	if os.IsNotExist(err) {
		if dir, _ := filepath.Split(path); len(dir) != 0 {
			if err = os.MkdirAll(dir, 0755); err != nil {
				return
			}
		}

		f, err := os.Create(path)

		if err == nil {
			f.Close()
		}

		return
	}

	if os.IsPermission(err) {
		os.Chmod(path, 0755)
	}
}

func watchEnvFile(path string, onchanges ...EnvEventFunc) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[yiigo] env watcher panic", zap.Any("error", r), zap.String("env_file", path), zap.ByteString("stack", debug.Stack()))
		}
	}()

	watcher, err := fsnotify.NewWatcher()

	if err != nil {
		logger.Error("[yiigo] env watcher error", zap.Error(err))

		return
	}

	defer watcher.Close()

	envDir, _ := filepath.Split(path)
	realEnvFile, _ := filepath.EvalSymlinks(path)

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer func() {
			wg.Done()

			if r := recover(); r != nil {
				logger.Error("[yiigo] env watcher panic", zap.Any("error", r), zap.String("env_file", path), zap.ByteString("stack", debug.Stack()))
			}
		}()

		writeOrCreateMask := fsnotify.Write | fsnotify.Create

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok { // 'Events' channel is closed
					return
				}

				eventFile := filepath.Clean(event.Name)
				currentEnvFile, _ := filepath.EvalSymlinks(path)

				// the env file was modified or created || the real path to the env file changed (eg: k8s ConfigMap replacement)
				if (eventFile == path && event.Op&writeOrCreateMask != 0) || (len(currentEnvFile) != 0 && currentEnvFile != realEnvFile) {
					realEnvFile = currentEnvFile

					if err := godotenv.Overload(path); err != nil {
						logger.Error("[yiigo] env reload error", zap.Error(err), zap.String("env_file", path))
					}

					for _, f := range onchanges {
						f(event)
					}
				} else if eventFile == path && event.Op&fsnotify.Remove&fsnotify.Remove != 0 {
					logger.Warn("[yiigo] env file removed", zap.String("env_file", path))
				}
			case err, ok := <-watcher.Errors:
				if ok { // 'Errors' channel is not closed
					logger.Error("[yiigo] env watcher error", zap.Error(err), zap.String("env_file", path))
				}

				return
			}
		}
	}()

	if err = watcher.Add(envDir); err != nil {
		logger.Error("[yiigo] env watcher error", zap.Error(err), zap.String("env_file", path))
	}

	wg.Wait()
}
