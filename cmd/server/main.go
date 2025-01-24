package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatei/libgo/server"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog/log"
)

func logRequests(next httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		ipv4 := r.Header.Get("X-Forwarded-For")
		log.Info().Str("endpoint", r.URL.String()).
			Str("method", r.Method).
			Str("ip", ipv4).
			Msg("Got request")
		next(w, r, params)
	}
}

type File struct {
	Name string `json:"name"`
	Size int64  `json:"bytes"`
}

func hasPermissions(user, project, path string) error {
	permissionsData, err := os.ReadFile(path)
	if err != nil {
		log.Error().Err(err).Str("path", path).Str("user", user).
			Msg("Failed to read user permissions")

		return err
	}

	permissions := strings.Split(string(permissionsData), "\n")
	found := false
	for _, permission := range permissions {
		if permission == project {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("%s isn't allowed in %s", user, path)
	}

	return nil
}

func createFile(cfg *Config) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		project := params.ByName("project")

		file, header, err := r.FormFile("source")
		if err != nil {
			log.Error().Err(err).Msg("Failed to read file from request")
			http.Error(w, `{"error": "failed to get source file"}`, http.StatusBadRequest)

			return
		}
		defer file.Close()

		user, found := strings.CutPrefix(r.Header.Get("Authorization"), "Token ")
		if !found {
			log.Error().Str("error", "No Token found").Msg("Failed authentication")
			http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)

			return
		}

		path := filepath.Join(cfg.Root, cfg.UsersDir, user, "projects")
		err = hasPermissions(user, project, path)
		if err != nil {
			log.Error().Err(err).Str("path", path).Str("user", user).Msg("Forbidden")
			http.Error(w, `{"error": "forbidden"}`, http.StatusForbidden)

			return
		}

		path = filepath.Join(cfg.Root, cfg.DataDir, project, header.Filename)

		dst, err := os.Create(path)
		if err != nil {
			log.Error().Err(err).Str("path", path).Msg("Failed to create file")
			http.Error(w, `{"error": "failed to create file"}`, http.StatusInternalServerError)

			return
		}
		defer dst.Close()

		_, err = dst.ReadFrom(file)
		if err != nil {
			log.Error().Err(err).Str("path", path).Msg("Failed to write file")
			http.Error(w, `{"error", "failed to write file"}`, http.StatusInternalServerError)

			return
		}
	}
}

func getFileList(cfg *Config) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		project := params.ByName("project")

		user, found := strings.CutPrefix(r.Header.Get("Authorization"), "Token ")
		if !found {
			log.Error().Str("error", "No Token found").Msg("Failed authentication")
			http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)

			return
		}

		path := filepath.Join(cfg.Root, cfg.UsersDir, user, "projects")
		err := hasPermissions(user, project, path)
		if err != nil {
			log.Error().Err(err).Str("path", path).Str("user", user).Msg("Forbidden")
			http.Error(w, `{"error": "forbidden"}`, http.StatusForbidden)

			return
		}

		path = filepath.Join(cfg.Root, cfg.DataDir, project)
		files, err := os.ReadDir(path)
		if err != nil {
			log.Error().Err(err).Str("path", path).Msg("Failed to list projects")
			http.Error(w, `{"error": "failed to list projects"}`, http.StatusInternalServerError)

			return
		}

		fileNames := []File{}
		for _, file := range files {
			info, err := file.Info()
			if err != nil {
				log.Error().Err(err).Str("path", file.Name()).Msg("Failed to get file info (size)")
				continue
			}

			fileNames = append(fileNames, File{
				Name: file.Name(),
				Size: info.Size(),
			})
		}

		output, err := json.Marshal(&fileNames)
		if err != nil {
			log.Error().Err(err).Str("path", path).Msg("Failed to produce output")
			http.Error(w, `{"error": "Failed to produce output"}`, http.StatusInternalServerError)

			return
		}

		_, err = w.Write(output)
		if err != nil {
			log.Error().Err(err).Msg("Failed to write response")
			http.Error(w, `{"error": "server error"}`, http.StatusInternalServerError)
		}
	}
}

func getFile(cfg *Config) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		project := params.ByName("project")
		fname := params.ByName("fname")

		user, found := strings.CutPrefix(r.Header.Get("Authorization"), "Token ")
		if !found {
			log.Error().Str("error", "No Token found").Msg("Failed authentication")
			http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)

			return
		}

		path := filepath.Join(cfg.Root, cfg.UsersDir, user, "projects")
		err := hasPermissions(user, project, path)
		if err != nil {
			log.Error().Err(err).Str("path", path).Str("user", user).Msg("Forbidden")
			http.Error(w, `{"error": "forbidden"}`, http.StatusForbidden)

			return
		}

		path = filepath.Join(cfg.Root, cfg.DataDir, project, fname)
		data, err := os.ReadFile(path)
		if err != nil {
			log.Error().Err(err).Str("path", path).Msg("Failed to read file")
			http.Error(w, `{"error": "failed to retrieve file"}`, http.StatusInternalServerError)

			return
		}

		_, err = w.Write(data)
		if err != nil {
			log.Error().Err(err).Msg("Failed to write response")
			http.Error(w, `{"error": "server error"}`, http.StatusInternalServerError)
		}
	}
}

type Config struct {
	Http     server.Config `toml:"http" yaml:"http"`
	Root     string        `toml:"root" yaml:"root"`
	DataDir  string        `toml:"data_dir" yaml:"data_dir"`
	UsersDir string        `toml:"users_dir" yaml:"users_dir"`
}

func newConfig(path string) *Config {
	var cfg Config
	err := cleanenv.ReadConfig(path, &cfg)
	if err != nil {
		log.Error().Err(err).Str("path", path).Msg("Failed to read config")

		return nil
	}

	return &cfg
}

func main() {
	cfg := newConfig("sample.toml")
	if cfg == nil {
		return
	}

	router := httprouter.New()
	router.POST("/:project", logRequests(createFile(cfg)))
	router.GET("/:project", logRequests(getFileList(cfg)))
	router.GET("/:project/:fname", logRequests(getFile(cfg)))

	log.Info().Str("conf", fmt.Sprintf("%v", cfg)).Msg("Starting")
	server.RunServer(&cfg.Http, router)
}
