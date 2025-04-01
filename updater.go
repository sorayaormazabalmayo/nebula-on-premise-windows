package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/stdr"
	"golang.org/x/oauth2/google"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/config"
	"github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

// The following config is used to fetch a target from Jussi's GitHub repository example
const (
	metadataURL          = "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/metadata"
	targetsURL           = "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/targets"
	verbosity            = 4
	generateRandomFolder = false
)

var (
	serviceAccountKeyPath = "C:\\nebula-on-premise-windows\\artifact-downloader-key.json"
	jsonFilePath          = "C:\\nebula-on-premise-windows\\update_status.json"
	service               = "nebula-on-premise-windows"
	targetIndexFile       = "C:\\nebula-on-premise-windows\\data\\nebula-on-premise-windows\\nebula-on-premise-windows-index.json"
	newBinaryPath         = "C:\\nebula-on-premise-windows\\tmp\\nebula-on-premise-linux.zip"
	destinationPath       = "C:\\nebula-on-premise-windows\\nebula-on-premise-linux.zip"
	SALTOLocation         = "C:\\nebula-on-premise-windows\\"
	windowsServiceName    = "Nebula1"
)

// struct to store update status
type UpdateStatus struct {
	UpdateAvailable int `json:"update_available"`
	UpdateRequested int `json:"update_requested"`
}

// indexInfo is the structure in which the information from the general-service.json is stored.
type indexInfo struct {
	Bytes  string `json:"bytes"`
	Path   string `json:"path"`
	Hashes struct {
		Sha256 string `json:"sha256"`
	} `json:"hashes"`
	Version     string `json:"version"`
	ReleaseDate string `json:"release-date"`
}

// Main program
func main() {

	// First, a lof file will be opened in append mode, create if does not exist

	// Setting Logger's file location
	logFileLocation := filepath.Join(SALTOLocation, "nebula_tuf_client.log")

	logFile, err := os.OpenFile(logFileLocation, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close() // Ensure file is closed when program exits

	// Create a MultiWriter to log to both stdout and file
	multiWriter := io.MultiWriter(os.Stdout, logFile)

	// Create logger 1 for TUF that writes to both destinations
	stdLogger1 := log.New(multiWriter, "Nebula TUF Client Logger:", log.LstdFlags)

	// Set logger to use both stdout and file
	metadata.SetLogger(stdr.New(stdLogger1))

	// Set verbosity level
	stdr.SetVerbosity(verbosity)

	// Retrieve and use logger
	tufLog := metadata.GetLogger()

	// Creating logger 2 for getting information of other staff not related as much to TUF
	generalLog := log.New(multiWriter, "Updater General Logger: ", log.LstdFlags)

	// initialize environment - temporary folders, etc.
	metadataDir, err := InitEnvironment()
	if err != nil {
		tufLog.Error(err, "Failed to initialize environment")
	}

	// initialize client with Trust-On-First-Use
	err = InitTrustOnFirstUse(metadataDir)
	if err != nil {
		tufLog.Error(err, "Trust-On-First-Use failed")
	}

	// getting the current version
	currentVersion, err := readCurrentVersion()

	if err != nil {
		generalLog.Printf("❌There has been an error wile reading the current version: %v❌\n", err)
	}

	generalLog.Printf("🟣Current Version is %s🟣\n", currentVersion)

	// getting the previous version folder
	previousVersion, err := getPreviousVersion(currentVersion)

	if err != nil {
		generalLog.Printf("❌There has been an error wile reading the previous version❌\n")
		generalLog.Printf("err:")
	}

	generalLog.Printf("🟣Previous Version is %s🟣\n", previousVersion)

	var wg sync.WaitGroup
	wg.Add(1)

	// Go routine 1 for setting the TUF updater
	go func() {
		defer wg.Done()

		// the updater needs to be looking for new updates every x time
		for {

			// downloading general-service-index.json
			_, foundDesiredTargetIndexLocally, err := DownloadTargetIndex(metadataDir, service)

			if err != nil {
				tufLog.Error(err, "Download index file failed")
			}

			// if there is a new one, this will mean that is initializing for the first time or that there is a new update
			if foundDesiredTargetIndexLocally == 0 && err == nil {
				err := setUpdateStatus(1)
				if err != nil {
					generalLog.Printf("❌ Error updating update_status.json: %v:", err)
				} else {
					generalLog.Printf("✅ Successfully set update_status.json to update_available: 1")
				}

			} else {
				generalLog.Printf("The local index file is the most updated one\n")
			}

			time.Sleep(time.Second * 60)

		}
	}()
	//

	// Go routine 2 that is alsways looking if the user has requested the update
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {

			// every x time it will be reading if the user has requested a new update
			updateRequested, err := ReadUpdateRequested(jsonFilePath)

			if err != nil {
				generalLog.Printf("There has been an error while reading the Update Requested Value: %f. \n", err)
			}

			// if the user has pushed the botton, the new server should be executed.
			if updateRequested == 1 {

				var data map[string]indexInfo

				generalLog.Printf("The index file is located in: %s \n", targetIndexFile)

				// read the actual JSON file content
				fileContent, err := os.ReadFile(targetIndexFile)
				if err != nil {
					generalLog.Printf("Fail to read the index file \n")
				}

				// parse JSON into the map
				err = json.Unmarshal(fileContent, &data)
				if err != nil {
					generalLog.Printf("\U0001F534Error parsing JSON: %v\U0001F534", err)
				}

				// getting service path
				servicePath := data[service].Path

				// download the artifact without specifying the file type
				err = downloadArtifact(serviceAccountKeyPath, servicePath, newBinaryPath, generalLog)
				if err != nil {
					generalLog.Printf("\U0001F534Failed to download binary: %v\U0001F534\n", err)
					os.Exit(1)
				}

				// make sure the new binary is executable
				err = os.Chmod(newBinaryPath, 0755)
				if err != nil {
					generalLog.Printf("Failed to set executable permissions \n")
				}

				// verifying that the downloaded file is integrate and authentic
				err = verifyingDownloadedFile(targetIndexFile, newBinaryPath, generalLog)

				if err == nil {
					// Replace old binary
					err = os.Rename(newBinaryPath, destinationPath)
					if err != nil {
						generalLog.Printf("Failed to rename the binary \n")
					}
				}

				serviceVersion := data[service].Version

				// unziping and setting the update status to 0
				unzipAndSetStatus(serviceVersion, generalLog)

				targetFileService := filepath.Join(SALTOLocation, serviceVersion, "bin", service)
				targetFileConfig := filepath.Join(SALTOLocation, serviceVersion, "config", "nebula-on-premise-linux.yml")

				// Updating where the Windows Service Manager should point

				// "C:\nebula-on-premise-windows\nebula-on-premise-windows.exe serve --config=C:\nebula-on-premise-windows\config\nebula-on-premise-windows.yml"

				newExecPath := fmt.Sprintf("%s serve --config=%s", targetFileService, targetFileConfig)

				if err := recreateService(windowsServiceName, newExecPath); err != nil {
					log.Fatalf("Service restart failed: %v", err)
				}
				log.Println("Service binpath updated and service restarted successfully.")

				// Deleting previous version's folder

				generalLog.Printf("🟣The previous version is %s🟣\n", previousVersion)

				previousVersionPath := filepath.Join(SALTOLocation, previousVersion)
				err = os.RemoveAll(previousVersionPath)

				generalLog.Printf("🟠Deleting previous version folder🟠\n")
				if err != nil {
					err = fmt.Errorf("error deleting the previous version's folder: %w", err)
					generalLog.Printf("Error: %w \n", err) // Print the error or handle it appropriately
				}

				// The previus version is what has been stored in current version
				previousVersion = currentVersion

				generalLog.Printf("🟣The previous version is %s🟣\n", previousVersion)

				currentVersion, err = readCurrentVersion()

				generalLog.Printf("🟣Current Version is %s🟣\n", currentVersion)

				if err != nil {
					generalLog.Printf("Error reading the current version")
				}

			}
			time.Sleep(time.Second * 5)
		}
	}()
	//
	wg.Wait()
}

// InitEnvironment prepares the local environment for TUF- temporary folders, etc.
func InitEnvironment() (string, error) {
	var tmpDir string

	if !generateRandomFolder {
		tmpDir = filepath.Join(SALTOLocation, "tmp")
		// create a temporary folder for storing the demo artifacts
		os.Mkdir(tmpDir, 0750)
	} else {
		// create a temporary folder for storing the demo artifacts
		_, err := os.MkdirTemp(SALTOLocation, "tmp")
		if err != nil {
			return "", fmt.Errorf("failed to create a temporary folder: %w", err)
		}
	}

	// create a destination folder for storing the downloaded target
	os.Mkdir(filepath.Join(SALTOLocation, "data"), 0750)
	return tmpDir, nil
}

// InitTrustOnFirstUse initialize local trusted metadata (Trust-On-First-Use)
func InitTrustOnFirstUse(metadataDir string) error {
	// check if there's already a local root.json available for bootstrapping trust
	_, err := os.Stat(filepath.Join(metadataDir, "root.json"))
	if err == nil {
		return nil
	}

	// download the initial root metadata so we can bootstrap Trust-On-First-Use
	rootURL, err := url.JoinPath(metadataURL, "1.root.json")
	if err != nil {
		return fmt.Errorf("failed to create URL path for 1.root.json: %w", err)
	}

	req, err := http.NewRequest("GET", rootURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}

	client := http.DefaultClient

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to executed the http request: %w", err)
	}

	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read the http request body: %w", err)
	}

	// write the downloaded root metadata to file
	err = os.WriteFile(filepath.Join(metadataDir, "root.json"), data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write root.json metadata: %w", err)
	}
	return nil
}

// Reading the version of the current running server. For that, the general_service_index.json
// version will be downloaded.

func readCurrentVersion() (string, error) {

	var data map[string]indexInfo

	// Read the actual JSON file content
	fileContent, err := os.ReadFile(targetIndexFile)
	if err != nil {
		return "", fmt.Errorf("failed to read index file: %w", err)
	}

	// Parse JSON into the map
	err = json.Unmarshal(fileContent, &data)
	if err != nil {
		return "", fmt.Errorf("error parsin the JSON: %w", err)
	}

	currentVersion := data[service].Version

	return currentVersion, nil
}

// getPreviousVersion gets the previous running version of the service.
// This will first read the folders that have version naming structure and the previous version will
// be the one that is different from the currentVersion
func getPreviousVersion(currentVersion string) (string, error) {
	var previousVersion string

	// Regular expression to match versioned folders
	versionRegex := regexp.MustCompile(`^v\d{4}\.\d{2}\.\d{2}-sha\.[a-fA-F0-9]{7}$`)

	// Read the directory
	entries, err := os.ReadDir(SALTOLocation)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}

	var versions []string

	// Filter versioned folders
	for _, entry := range entries {
		if entry.IsDir() && versionRegex.MatchString(entry.Name()) {
			versions = append(versions, entry.Name())
		}
	}

	// Ensure we have exactly two versions
	if len(versions) != 2 {
		return "", fmt.Errorf("expected 2 versioned folders, found %d", len(versions))
	}

	// Identify the previous version (the one different from currentVersion)
	for _, version := range versions {
		if version != currentVersion {
			previousVersion = version
			break
		}
	}

	if previousVersion == "" {
		return "", fmt.Errorf("previous version not found")
	}

	return previousVersion, nil
}

// DownloadTargetIndex downloads the target file using Updater. The Updater refreshes the top-level metadata,
// get the target information, verifies if the target is already cached, and in case it
// is not cached, downloads the target file.
func DownloadTargetIndex(localMetadataDir, service string) ([]byte, int, error) {

	//serviceFilePath := filepath.Join(service, fmt.Sprintf("%s-index.json", service))

	serviceFilePath := fmt.Sprintf("%s/%s-index.json", service, service)

	rootBytes, err := os.ReadFile(filepath.Join(localMetadataDir, "root.json"))
	if err != nil {
		return nil, 0, err
	}

	// create updater configuration
	cfg, err := config.New(metadataURL, rootBytes) // default config
	if err != nil {
		return nil, 0, err
	}

	cfg.LocalMetadataDir = localMetadataDir
	cfg.LocalTargetsDir = filepath.Join(SALTOLocation, "data")
	cfg.RemoteTargetsURL = targetsURL
	cfg.PrefixTargetsWithHash = true

	// create a new Updater instance
	up, err := updater.New(cfg)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create Updater instance: %w", err)
	}

	// try to build the top-level metadata
	err = up.Refresh()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to refresh trusted metadata: %w", err)
	}

	// Decode serviceFilePath before calling GetTargetInfo
	decodedServiceFilePath, _ := url.QueryUnescape(serviceFilePath)

	// Get metadata info
	ti, err := up.GetTargetInfo(decodedServiceFilePath)
	if err != nil {
		return nil, 0, fmt.Errorf("getting info for target index \"%s\": %w", serviceFilePath, err)
	}

	os.Mkdir(filepath.Join(SALTOLocation, "data", service), 0750)

	targetFilePath := filepath.Join(SALTOLocation, "data", service, fmt.Sprintf("%s-index.json", service))
	os.MkdirAll(filepath.Dir(targetFilePath), 0750) // Ensure the directory exists

	path, tb, err := up.FindCachedTarget(ti, targetFilePath)

	if err != nil {
		return nil, 0, fmt.Errorf("failed to find if there is a cachet target: %w", err)
	}

	if path != "" {
		// Cached version found
		fmt.Println("\U0001F34C CACHE HIT")
		return tb, 1, nil
	}

	// Ensure it is unescaped
	decodedTargetFilePath, _ := url.QueryUnescape(targetFilePath)

	// Now download
	targetfilePath, tb, err := up.DownloadTarget(ti, decodedTargetFilePath, "")
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download target index file %s - %w", service, err)
	}

	fmt.Printf(" 🎯📄The target File Path is: %s 🎯📄", targetfilePath)

	return tb, 0, nil
}

// Function to update update_status.json
func setUpdateStatus(value int) error {
	// Create struct with new value
	updateStatus := UpdateStatus{UpdateAvailable: value}

	// Convert struct to JSON
	file, err := json.MarshalIndent(updateStatus, "", "  ")
	if err != nil {
		return err
	}

	// Write JSON to file
	err = os.WriteFile(jsonFilePath, file, 0644)
	if err != nil {
		return err
	}
	return nil
}

// ReadUpdateRequested extracts the "update_requested" value from a JSON file
func ReadUpdateRequested(jsonFilePath string) (int, error) {
	// Read the JSON file content
	fileContent, err := os.ReadFile(jsonFilePath)
	if err != nil {
		return 0, fmt.Errorf("failed to read JSON file: %v", err)
	}

	// Unmarshal JSON into struct
	var status UpdateStatus
	err = json.Unmarshal(fileContent, &status)
	if err != nil {
		return 0, fmt.Errorf("error parsing JSON: %v", err)
	}

	return status.UpdateRequested, nil
}

// Downloading the artifact indicated in general-service.json
func downloadArtifact(serviceAccountKeyPath, servicePath, newBinaryPath string, generalLog *log.Logger) error {
	// Authenticate using the service account key
	ctx := context.Background()
	creds, err := google.CredentialsFromJSON(ctx, readFile(serviceAccountKeyPath, generalLog), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return fmt.Errorf("failed to load service account credentials: %w", err)
	}

	// Create HTTP client with the token
	client := &http.Client{}
	req, err := http.NewRequest("GET", servicePath, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add Authorization header with Bearer token
	token, err := creds.TokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to retrieve token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	// Perform the request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download artifact, status code: %d", resp.StatusCode)
	}

	// Determine the file name from the Content-Disposition header or use a default name
	contentDisposition := resp.Header.Get("Content-Disposition")
	fileName := newBinaryPath
	if contentDisposition != "" {
		_, params, err := mime.ParseMediaType(contentDisposition)
		if err == nil {
			if name, ok := params["filename"]; ok {
				fileName = name
			}
		}
	}
	generalLog.Printf("Saving file as: %s\n", fileName)

	// Write the response to a file
	out, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// readFile reads the content of the service account key JSON file.
func readFile(path string, generalLog *log.Logger) []byte {
	content, err := os.ReadFile(path)
	if err != nil {
		generalLog.Printf("\U0001F534Error reading file %s: %v\U0001F534\n", path, err)
		os.Exit(1)
	}
	return content
}

// verifyingDownloadedFile verifies a file.
func verifyingDownloadedFile(targetIndexFile, DonwloadedFilePath string, generalLog *log.Logger) error {

	var data map[string]indexInfo

	// Read the actual JSON file content
	fileContent, err := os.ReadFile(targetIndexFile)
	if err != nil {
		return fmt.Errorf("failed to read index file: %w", err)
	}

	// Parse JSON into the map
	err = json.Unmarshal(fileContent, &data)
	if err != nil {
		generalLog.Printf("\U0001F534Error parsing JSON: %v\U0001F534", err)
		return err
	}

	indexHash := data[service].Hashes.Sha256

	generalLog.Printf("The hash from the nebula-service-index.json is %s", indexHash)

	// Computing the hash of the downloaded file

	// Compute the SHA256 hash
	downloadedFilehash, err := ComputeSHA256(DonwloadedFilePath)

	generalLog.Printf("Downloaded file hash is: %s\n", downloadedFilehash)

	if err != nil {
		generalLog.Printf("\U0001F534Error computing hash: %v\U0001F534\n", err)
		return fmt.Errorf("error while computing the hash")
	}

	if indexHash == downloadedFilehash {
		generalLog.Printf("\U0001F7E2The target file has been downloaded successfully!\U0001F7E2\n")
	} else {
		return fmt.Errorf("there has been an error while downloading the file, the hashes do not match")
	}
	return nil
}

// Computing the SHA256 of a file.
func ComputeSHA256(filePath string) (string, error) {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create a SHA256 hash object
	hasher := sha256.New()

	// Copy the file contents into the hasher
	// This reads the file in chunks to handle large files efficiently
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to compute hash: %w", err)
	}

	// Get the final hash as a byte slice and convert to a hexadecimal string
	hash := hasher.Sum(nil)
	return fmt.Sprintf("%x", hash), nil
}

// Unzipping the downloaded target and setting the update status to 0.
func unzipAndSetStatus(serviceVersion string, generalLog *log.Logger) {

	destinationPathUnzip := ""
	destinationPathUnzip = fmt.Sprintf("%s/%s", SALTOLocation, serviceVersion)

	// Unzipping the downloaded target
	if err := Unzip(destinationPath, destinationPathUnzip); err != nil {
		generalLog.Printf("❌ Error unzipping new binary: %v", err)
	} else {
		generalLog.Printf("✅ Successfully unzipped the new binary.")
	}

	// Removing what has been unzipped
	os.Remove(destinationPath)

	// Setting update status to 0
	setUpdateStatus(0)

}

// Unzipping a .zip and relocating it.
func Unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := r.Close(); err != nil {
			panic(err)
		}
	}()

	os.MkdirAll(dest, 0755)

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() {
			if err := rc.Close(); err != nil {
				panic(err)
			}
		}()

		path := filepath.Join(dest, f.Name)

		// Check for ZipSlip (Directory traversal)
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", path)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
		} else {
			os.MkdirAll(filepath.Dir(path), f.Mode())
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					panic(err)
				}
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range r.File {
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}
	return nil
}

// startService starts a service
func startService(serviceName string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()

	return s.Start()
}

// stopService stops a service
func stopService(serviceName string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()

	// Send stop command
	status, err := s.Control(svc.Stop)
	if err != nil {
		return err
	}
	log.Printf("Service status: %+v\n", status)
	return nil
}

// deleteService deletes the service
func deleteService(serviceName string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer s.Close()

	return s.Delete()
}

// waitForServiceState polls the service status until it reaches the desired state or the timeout expires.
func waitForServiceState(s *mgr.Service, state svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := s.Query()
		if err != nil {
			return err
		}
		if status.State == state {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for service to reach state %v", state)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// recreateService deletes the existing service (if any) and creates a new one with the specified binary path.
func recreateService(serviceName, newExePath string) error {
	// Connect to the Service Manager.
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %v", err)
	}
	defer m.Disconnect()

	// Try opening the service to see if it exists.
	s, err := m.OpenService(serviceName)
	if err == nil {
		// If the service exists, stop it (ignoring errors if it's not running).
		_, _ = s.Control(svc.Stop)
		// Optionally, wait for the service to stop.
		_ = waitForServiceState(s, svc.Stopped, 30*time.Second)

		// Delete the service.
		if err := s.Delete(); err != nil {
			s.Close()
			return fmt.Errorf("failed to delete service %s: %v", serviceName, err)
		}
		s.Close()
	} else {
		// If the service doesn't exist, that's fine. We can create a new one.
		log.Printf("Service %s does not exist, will create a new one", serviceName)
	}

	// Create a new service with the desired binary path (including command-line arguments).
	s, err = m.CreateService(serviceName, newExePath, mgr.Config{
		DisplayName: serviceName,
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		return fmt.Errorf("failed to create service: %v", err)
	}
	defer s.Close()

	// Optionally, set a service description.
	// _ = s.SetDescription("Your service description here")

	// Start the newly created service.
	if err := s.Start(); err != nil {
		return fmt.Errorf("failed to start service: %v", err)
	}

	return nil
}
