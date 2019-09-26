package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/gorilla/mux"
)

var (
	flagAddr         = flag.String("addr", ":8080", "Server address")
	flagPatchCache   = flag.String("patchcache", ".", "Path to the patch cache")
	flagTarballCache = flag.String("tarcache", ".", "Path to the tarball cache")
	flagMaxBytesBody = flag.Int("maxbytesbody", 250*1024, "Maximum nunber of bytes in the body")
	flagKeepTmpDir   = flag.Bool("keeptmpdir", false, "Do not delete the temporary directory, useful for debugging")
	flagCertFile     = flag.String("certfile", "", "Path to a TLS cert file")
	flagKeyFile      = flag.String("keyfile", "", "Path to a TLS key file")
)

const tarballURLBase = "https://www.linbit.com/downloads/drbd/"

type server struct {
	router *mux.Router

	// probably stat() would be good enough
	// keys are the complete file system path to the cached patch
	patchCache     map[string]int
	patchCachePath string

	// keys are the complete file system path to the tarball
	tarballCache     map[string]struct{}
	tarballCachePath string

	pl, tl sync.RWMutex // patch lock/tarball lock

	maxBytesBody int64
	keepTmpDir   bool
}

func main() {
	flag.Parse()

	if *flagMaxBytesBody < 0 {
		log.Fatal("maxbytesbody has to be a positive value")
	}

	s := &server{
		router: mux.NewRouter(),

		patchCachePath: *flagPatchCache,
		patchCache:     make(map[string]int),

		tarballCachePath: *flagTarballCache,
		tarballCache:     make(map[string]struct{}),

		maxBytesBody: int64(*flagMaxBytesBody),
		keepTmpDir:   *flagKeepTmpDir,
	}

	if err := s.updatePatchCache(); err != nil {
		log.Fatal(err)
	}
	if err := s.updateTarballCache(); err != nil {
		log.Fatal(err)
	}
	s.routes()

	server := http.Server{
		Addr:           *flagAddr,
		Handler:        s,
		MaxHeaderBytes: 4 * 1024,
	}

	if *flagCertFile != "" && *flagKeyFile != "" {
		log.Fatal(server.ListenAndServeTLS(*flagCertFile, *flagKeyFile))
	} else {
		log.Fatal(server.ListenAndServe())
	}
}

// handler interface, wrapped for MaxBytesReader
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBytesBody)
	s.router.ServeHTTP(w, r)
}

func (s *server) updatePatchCache() error {
	s.pl.Lock()
	defer s.pl.Unlock()

	if _, err := os.Stat(s.patchCachePath); os.IsNotExist(err) {
		if err := os.MkdirAll(s.patchCachePath, 0755); err != nil {
			return fmt.Errorf("Patch cache directory '%s' did not exist and could not be created: %v", s.patchCachePath, err)
		}
	}

	return filepath.Walk(s.patchCachePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(info.Name(), ".patch") {
			if abs, err := filepath.Abs(path); err == nil {
				log.Println("adding", abs, "to the patch cache")
				s.patchCache[abs] = 0
			}
		}
		return nil
	})
}

func (s *server) updateTarballCache() error {
	if _, err := os.Stat(s.tarballCachePath); os.IsNotExist(err) {
		if err := os.MkdirAll(s.tarballCachePath, 0755); err != nil {
			return fmt.Errorf("Tarball cache directory '%s' did not exist and could not be created: %v", s.tarballCachePath, err)
		}
	}

	matches, err := filepath.Glob(s.tarballCachePath + "/drbd*.tar.gz")
	if err != nil {
		return err
	}

	s.tl.Lock()
	for _, f := range matches {
		abs, err := filepath.Abs(f)
		if err != nil {
			continue
		}
		s.tarballCache[abs] = struct{}{}
		log.Println("adding", abs, "to the tarball cache")
	}
	s.tl.Unlock()
	return nil
}

func (s *server) spatchCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/text")

		drbdversion, ok := mux.Vars(r)["drbdversion"]
		if !ok || drbdversion == "" || len(drbdversion) > 42 {
			errorf(http.StatusBadRequest, w, "Could not get valid drbdversion parameter")
			return
		}

		patch, err := s.genPatch(r, drbdversion)
		if err != nil {
			errorf(http.StatusBadRequest, w, "Could not generate patch: %v", err)
			// TODO(rck): maybe from this point on internal server error? We would need to differentiate
			return
		}

		if _, err := fmt.Fprintf(w, string(patch)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func fetchDRBDTarball(tarballName, dst string) error {
	vers := "9.0"
	re := regexp.MustCompile(`^drbd-([0-9]+)\.([0-9]+)\..*\.tar\.gz$`)
	if res := re.FindStringSubmatch(tarballName); res != nil {
		vers = fmt.Sprintf("%s.%s", res[1], res[2])
	}

	url := fmt.Sprintf("%s/%s/%s", tarballURLBase, vers, tarballName)
	log.Println("Fetching", url)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return errors.New("Could not fetch tarball")
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, resp.Body)
	return err
}

func (s *server) newPatch(body []byte, drbdversion string) ([]byte, error) {
	compath, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil {
		return nil, fmt.Errorf("Could not base64 decode body: %v", err)
	}

	// minimal sanity check:
	if c := bytes.Count(compath, []byte{'\n'}); c < 5 || c > 200 {
		return nil, fmt.Errorf("Decoded compat.h had invalid number of lines: %d", c)
	}

	intarballName := "drbd-" + drbdversion
	tgzName := intarballName + ".tar.gz"
	tarballPath := filepath.Join(s.tarballCachePath, tgzName)
	tarballPath, err = filepath.Abs(tarballPath)
	if err != nil {
		return nil, err
	}

	s.tl.Lock()
	_, cached := s.tarballCache[tarballPath]
	if !cached {
		if err := fetchDRBDTarball(tgzName, tarballPath); err != nil {
			s.tl.Unlock()
			return nil, err
		}
		s.tarballCache[tarballPath] = struct{}{}
	}
	s.tl.Unlock()

	dir, err := ioutil.TempDir("", "saas")
	if err != nil {
		return nil, fmt.Errorf("Could not create temporary directory: %v", err)
	}

	if !s.keepTmpDir {
		defer func() { _ = os.RemoveAll(dir) }()
	}

	cmd := exec.Command("tar", "xf", tarballPath, "-C", dir)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Could not extract tarball: %v", err)
	}

	// '_' is never an existing md5sum
	cocciPath := filepath.Join(dir, intarballName, "drbd", "drbd-kernel-compat", "cocci_cache", "_")
	if err := os.Mkdir(cocciPath, 0755); err != nil {
		return nil, fmt.Errorf("Could not create cocci dir: %v", err)
	}
	if err := ioutil.WriteFile(filepath.Join(cocciPath, "compat.h"), compath, 0644); err != nil {
		return nil, fmt.Errorf("Could not write compath.h: %v", err)
	}
	if err := ioutil.WriteFile(filepath.Join(cocciPath, "kernelrelease.txt"), []byte{'_'}, 0644); err != nil {
		return nil, fmt.Errorf("Could not write kernelrelease.txt: %v", err)
	}

	cmd = exec.Command("make", "-C", filepath.Join(dir, intarballName, "drbd"), "compat")
	cmd.Stdin = os.Stdin // otherwise spatch fails if no tty
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Could not run make:%s %v", filepath.Join(dir, intarballName, "drbd"), err)
	}

	return ioutil.ReadFile(filepath.Join(cocciPath, "compat.patch"))
}

func (s *server) genPatch(r *http.Request, drbdversion string) ([]byte, error) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	h := sha1.New()
	h.Write(body) // the still base64 encoded one.
	patchName := fmt.Sprintf("%x.patch", h.Sum(nil))
	patchDir := filepath.Join(s.patchCachePath, drbdversion)
	patchPath := filepath.Join(patchDir, patchName)
	patchPath, err = filepath.Abs(patchPath)
	if err != nil {
		return nil, err
	}

	s.pl.Lock()
	n, cached := s.patchCache[patchPath]
	log.Printf("Patch '%s'; reqest #%d", patchName, n+1)
	if cached {
		s.patchCache[patchPath]++
		s.pl.Unlock()
		return ioutil.ReadFile(patchPath)
	}
	s.pl.Unlock()

	patch, err := s.newPatch(body, drbdversion)
	if err != nil {
		return nil, err
	}

	// add to cache
	s.pl.Lock()
	defer s.pl.Unlock()
	// don't fail hard, just don't add to cache
	if _, err := os.Stat(patchDir); os.IsNotExist(err) {
		if err := os.Mkdir(patchDir, 0755); err != nil {
			log.Println("Could not create patch cache directory")
			return patch, nil
		}
	}
	if err := ioutil.WriteFile(patchPath, patch, 0644); err != nil {
		// remove file so it does not land in the cache next time
		if st, err := os.Stat(patchPath); err != nil && st.Mode().IsRegular() {
			if err := os.Remove(patchPath); err != nil {
				log.Printf("Critical - Could not delete broken cache file: %s", patchPath)
			}
		}
		return patch, nil
	}
	s.patchCache[patchPath] = 1

	return patch, nil
}

func errorf(code int, w http.ResponseWriter, format string, a ...interface{}) {
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, format, a...)
	log.Printf(format, a...)
}
