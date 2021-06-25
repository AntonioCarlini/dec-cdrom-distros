// This program is intended to help maintain a YAML file taht contains details of manually verified SPD files.
// This file was originally created by massaging the output of the "rejected SPDs" from spds-to-yaml.go.
// Some entries have then been modified manually with information gleaned from the SPD text file or its
// PostScript equivalent. Many files are just assumed to have no data for now. Even that allows
// spds-to-yaml.go to not mark them as "bad, in need of attention" so that newly added SPD files
// that fail validation are easier to spot.
//
// The output file will always be sorted by map key in alphabetical order.
// The only change to the data is the possible generation of an MD5 checksum if nonbe was present before.
// Existing checksums are not currently checked.
// Statistics are always displayed, even in non-verbose mode.
//
// CLI options:
// --verbose
//   Enable verbose reporting. As this produces a number of lines of output for each SPD file, this is probably most useful when running against a small number of SPDs.
// --spds-file filepath
//   filepath of the YAML file to analyse
// --output filepath
//   filepath of the output YAML file that contains any corrections (e.g. addition of checksums)
//

package main

import (
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"io/ioutil"
	"log"

	"gopkg.in/yaml.v3"
)

// struct to hold information about _SPD.TXT files that are difficult to analyse automatically or have no valid information
type ManualSpdInfo struct {
	Filename    string // Filename (no path info)
	Id          string // SPD ID (or empty string if none available)
	Title       string // SPD title (or empty string if none available)
	Part_num    string // SPD document Part number
	Date        string // SPD date in YYYY-MM format
	Reason      string // reason for special handling (e.g. unknown character encoding)
	Path        string // path to file
	Md5Checksum string // MD5 checksum of file, or nil if not knownn
}

type ManualSpdFiles map[string]ManualSpdInfo // map of filename => ManualSpdInfo object

func main() {

	// Parse and check the
	verbose := flag.Bool("verbose", false, "Enable verbose reporting")
	manualSpdFilename := flag.String("spds-file", "", "File that contains the YAML for manually handled SPDs")
	outputFilename := flag.String("output", "/dev/null", "File that contains the updated YAML for manually handled SPDs")
	flag.Parse()

	if *manualSpdFilename == "" {
		fmt.Println("Please supply a filespec for the output YAML")
		log.Fatal("Missing required argument")
	}

	// Read in the data that identifies manually handled SPD files
	manualSpdsYamlData, err := ioutil.ReadFile(*manualSpdFilename)
	if err != nil {
		log.Fatal(err)
	}

	var manualSpdFiles ManualSpdFiles
	err = yaml.Unmarshal(manualSpdsYamlData, &manualSpdFiles)
	if err != nil {
		log.Fatal(err)
	}

	// Initalise statistics
	statsInputEntries := len(manualSpdFiles)
	statsNoSpdId := 0
	statsNoTitle := 0
	statsNoPartNum := 0
	statsNoDate := 0
	statsNoChecksum := 0
	statsGeneratedChecksums := 0
	statsEntriesDeleted := 0

	// Checks:
	// o Every entry must have an MD5 checksum: generate one if necessary.
	// o Eliminate entries that have the same checksum *and* filename.
	//   These are likely to be the same file appearing on multiple CDROM distributions.
	// o There may be multpile files with the same name but a *different* checksum.
	//   So far all of these have been identical files where perhaps the product name has changed,

	dataModified := false
	checksums := make(map[string]string) // map of checksum => filename
	for name, entry := range manualSpdFiles {
		if entry.Md5Checksum == "" {
			if *verbose {
				fmt.Println("Generating an MD5 checksum for ", entry.Path)
			}
			fileBytes, err := ioutil.ReadFile(entry.Path)
			if err != nil {
				log.Fatal(err)
			}
			md5Hash := md5.Sum(fileBytes)
			value := entry
			value.Md5Checksum = hex.EncodeToString(md5Hash[:])
			entry = value
			statsGeneratedChecksums++
			dataModified = true
		}
		if filename, checksum_present := checksums[entry.Md5Checksum]; checksum_present {
			// Duplicate checksum
			if entry.Filename != filename {
				fmt.Println("Both ", filename, "and", entry.Filename, " have the same checksum: ", entry.Md5Checksum)
			} else {
				if *verbose {
					fmt.Println("Dropping ", entry.Path, ": duplicate checksum: ", entry.Md5Checksum)
				}
				delete(manualSpdFiles, name)
			}
		} else {
			// Unique checksum (so far, at least)
			checksums[entry.Md5Checksum] = entry.Filename
		}
		// Gather further stats
		if entry.Id == "" {
			statsNoSpdId++
		} else {
			if entry.Title == "" {
				statsNoTitle++
			}
			if entry.Part_num == "" {
				statsNoPartNum++
			}
			if entry.Date == "" {
				statsNoDate++
			}
			if entry.Md5Checksum == "" {
				statsNoChecksum++
			}
		}
	}

	// If necessary, write out the modified data
	if dataModified {
		outputData, err := yaml.Marshal(&manualSpdFiles)
		if err != nil {
			log.Fatal(err)
		}
		err = ioutil.WriteFile(*outputFilename, outputData, 0644)
		if err != nil {
			log.Fatal(err)
		}

	}

	// Display statistics
	fmt.Println("Total entries (initial):        ", statsInputEntries)
	fmt.Println("Total entries (final):          ", len(manualSpdFiles))
	fmt.Println("Invalid entries (no SPD ID):    ", statsNoSpdId)
	fmt.Println("Valid entries with no title:    ", statsNoTitle)
	fmt.Println("Valid entries with no part num: ", statsNoPartNum)
	fmt.Println("Valid entries with no date:     ", statsNoDate)
	fmt.Println("Valid entries with no checksum: ", statsNoChecksum)
	fmt.Println("Unique checksums:               ", len(checksums))
	fmt.Println("Generated checksums:            ", statsGeneratedChecksums)
	fmt.Println("Entries deleted:                ", statsEntriesDeleted)
}
