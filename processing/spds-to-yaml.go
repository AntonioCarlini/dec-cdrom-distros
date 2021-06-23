// This program reads one or more SPDs in text format and produces a YAML file that lists the following data for each SPD:
//  - SPD ID (in the format xx.xx.xx) [REQUIRED]
//  - Title [Always present but not validated at all]
//  - Part number (in the usual DEC format xx-xxxxx-xx) [OPTIONAL]
//  - Date (in the format YYYY-MM) [OPTIONAL]
//  - Notes field, containing the identification of the CDROM disc
//
// The intention is to reduce the SPD files to a small amount of metadata describing each one that can then be used for further processing.
//
// CLI options:
// --cdroms filepath
//   filepath of the input file that describes the known CDROM discs
// --yaml filepath
//   filepath of the output file to hold the generated yaml
// --months filepath
//   filepath of YAML file that provides month name to month number in various languages (default "months.yaml")
// --statistics
//   Display a statistics summary
// --bad-spds
//   Display a list of SPDs that completely failed validation
// --missing-date
//   Display SPDs without a date
// --missing-part-num
//   Display SPDs without a part number
// --verbose
//   Enable verbose reporting. As this produces a number of lines of output for each SPD file, this is probably most useful when running against a small number of SPDs.
//  Everything else is a list of presumed SPD files to process.
//
// For each file:
// o look for text between 'PRODUCT NAME:' and 'DESCRIPTION'
//   (Allow for other languages, but the general pattern is the same)
// o extract (and remove) 'SPD xx.xx.xx'.
//   (Note that *some* French SPDs use "DPL" and others used "SPD")
// o everything else (with whitespace collapsed and the SPD xx.xx.xx removed) is the title
// o Then either on first page:
//     'DIGITAL' 'month YYYY'
//     AE-xxxxx-TC (more generally xx-xxxxx-xx)
//     ^L  <-- line feed marking end of page
//   or at the very end
//     'month YYYY'
//      xx-xxxxx-xx
//      possible whitespace and lines but no text
//
// Points that may need further investigation are highlighted using "ISSUE: " at the beginning of the warning, even if "--verbose" is not used.
// Further information is only displayed if "--verbose" is specified.
//
// Notes so far:
// o Some SPDs, e.g. VAX FORTRAN SPD do not say "PRODUCT NAME".
//   Allow for these by spotting that all other attemps at recognition have failed, then fall back to just looking for "DESCRIPTION" or "PRODUCT DESCRIPTION" alone on a line.
// o Some SPDS have an SSA appended. This causes confusion with the validation code as the SSA may have its own part number and date either of which may be different to those of the SPD.
//   To avoid this, the SSA text is stripped. DECISIONDE011_SPD.TXT and DECWDE_DE021_SPD.TXT don't undergo this process as the German language indication of the beginning of an SSA is
//   such that I've yet to find a safe and reliable way of detecting it.
// o Japanese language SPDs fail validation as their character set is not recognised.
// o Some files fail validation even though they could be detected with further work (e.g. DECISIONES011_SPD.TXT) but it seems simpler to catch those at the end of the process and
//   handle them manuall if necessary.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// struct to hold SPD information
type SPD_Entry struct {
	ID       string // The SPD ID of the form ab.cd.ef
	Title    string // The text title of the SPD
	Part_num string // The DEC part#: ab-cdefg-hj
	Date     string // yyyy-mm never bother with an actual day, even in the unlikely event that one is present
	Notes    string // Free form notes
	path     string // Path to SPD file (note: lowercase member name so that it will not be exported to the YAML output)
}

type CDROM struct {
	Part_num string // DEC 2-5-2 part number
	Title    string // CDROM title
	Number   int    // series member (i.e. the 3 in "3 of 7")
	Total    int    // total member (i.e. the 7 in "3 of 7")
	Date     string // Date on CDROM in format YYYY-MM
	Volume   string // Volume label
	Format   string // FS format, e.g. ODS-2
	Path     string // Directory path
}

type PathToCDROM map[string]CDROM
type MonthNameToNumber map[string]string

func main() {
	verbose := flag.Bool("verbose", false, "Enable verbose reporting")
	listBadSPDs := flag.Bool("bad-spds", false, "Display a list of SPDs that completely failed validation")
	listStatistics := flag.Bool("statistics", false, "Display a statistics summary")
	listSPDsWithNoPartNum := flag.Bool("missing-part-num", false, "Display SPDs without a part number")
	listSPDsWithNoDate := flag.Bool("missing-date", false, "Display SPDs without a date")
	yamlOutputFilename := flag.String("yaml", "", "filepath of the output file to hold the generated yaml")
	cdromsFilename := flag.String("cdroms", "", "filepath of the input file that describes the known CDROM discs")
	monthsFilename := flag.String("months", "months.yaml", "filepath of YAML file that provides month name to month number in various languages")
	flag.Parse()

	args_error := false

	if *yamlOutputFilename == "" {
		fmt.Println("Please supply a filespec for the output YAML")
		args_error = true
	}

	if *cdromsFilename == "" {
		fmt.Println("Please supply a filespec for the CDROM disc data file")
		args_error = true
	}

	if args_error {
		os.Exit(2)
	}

	inputFiles := flag.Args()

	if len(inputFiles) == 0 {
		fmt.Println("At least one input file is required.")
		os.Exit(3)
	}

	pathToCDROM := build_path_to_CDROM_map(*cdromsFilename)
	monthNamesToDigits := build_month_names_to_digits_map(*monthsFilename)

	var spdEntries []SPD_Entry
	var badSPDs []SPD_Entry

	for _, spdFilename := range inputFiles {
		if *verbose {
			fmt.Println("+++++++++++++++++++++++++++++++++++++++++++++++++++++++++")
			fmt.Println("Processing SPD: [" + spdFilename + "]")
		}
		thisSPD := decode_spd_file(spdFilename, pathToCDROM, monthNamesToDigits, *verbose)
		if thisSPD.ID == "FAILED" {
			badSPDs = append(badSPDs, thisSPD)
		} else {
			spdEntries = append(spdEntries, thisSPD)
		}
		if *verbose {
			fmt.Println("=========================================================")
		}
	}

	validButNoDate := 0
	validButNoPartNum := 0
	for _, spd := range spdEntries {
		if *verbose {
			fmt.Println("SPD:  ID", spd.ID, "T:", spd.Title, "#:", spd.Part_num, "D:", spd.Date, "N:", spd.Notes)
		}
		if spd.Part_num == "" {
			validButNoPartNum++
		}
		if spd.Date == "" {
			validButNoDate++
		}
	}

	if *listStatistics {
		fmt.Println("Total Good SPDs:                  ", len(spdEntries))
		fmt.Println("Total good SPDs without a date:   ", validButNoDate)
		fmt.Println("Total good SPDs without a part #: ", validButNoPartNum)
		fmt.Println("Total REJECTED SPDs:              ", len(badSPDs))
	}

	if *listSPDsWithNoPartNum {
		fmt.Println("These SPDs are valid but no part number (XX-XXXXX-XX) was found:")
		for _, spd := range spdEntries {
			if spd.Part_num == "" {
				fmt.Println("SPD:  ID", spd.ID, "T:", spd.Title, "#:", spd.Part_num, "D:", spd.Date, "P:", spd.path)
			}
		}
	}

	if *listSPDsWithNoDate {
		fmt.Println("These SPDs are valid but no date was found:")
		for _, spd := range spdEntries {
			if spd.Date == "" {
				fmt.Println("SPD:  ID", spd.ID, "T:", spd.Title, "#:", spd.Part_num, "D:", spd.Date, "P:", spd.path)
			}
		}
	}

	if *verbose || *listBadSPDs {
		fmt.Println("These SPDs failed validation (no ID found):")
		for _, spd := range badSPDs {
			fmt.Println("SPD:  ID", spd.ID, "T:", spd.Title, "#:", spd.Part_num, "D:", spd.Date, "N:", spd.Notes)
		}
	}

	data, err := yaml.Marshal(&spdEntries)
	if err != nil {
		log.Fatal(err)
	}

	err = ioutil.WriteFile(*yamlOutputFilename, data, 0644)
	if err != nil {
		log.Fatal(err)
	}
}

// Read the specified file and examine the text to locate the information necessary to create an SPD_Entry.
// The SPD ID (i.e. "SPD xx.xx.xx") is mandatory, as is the title.
// Date and Part # are optional as there are SPDs that don't have one or the other (or even both).
// The Notes field will be filled in with an indetifier that is close to what would be on the physical CDROM label.
func decode_spd_file(spdFilename string, pathToCDROM PathToCDROM, monthNamesToDigits MonthNameToNumber, verbose bool) SPD_Entry {
	// Read the SPD file
	bytes, err := ioutil.ReadFile(spdFilename)
	if err != nil {
		log.Fatal(err)
	}

	// Eliminate any trailing System Support Addendum text that may be appended.
	// Everything from the first occurrence of
	//    System
	//    Support
	//    Addendum
	// is eliminated.
	spd_text := string(bytes)
	re_ssa := regexp.MustCompile(`(?ms)(\A.*?)(^\s*System\s*$\s*Support\s*$\s*Addendum\s*$.*)`)
	drop_ssa := re_ssa.FindAllStringSubmatch(spd_text, -1)
	if len(drop_ssa) > 0 {
		spd_text = drop_ssa[0][1]
	}
	spd := SPD_Entry{ID: "FAILED", Title: "", Part_num: "", Date: "", Notes: "", path: spdFilename}

	// Find the subdirectory, which can be decoded into a CDROM disc identifier
	file_dir, _ := path.Split(spdFilename)
	_, last_path := path.Split(strings.TrimSuffix(file_dir, "/"))

	// Find the SPD ID and the SPD title
	re_spaces := regexp.MustCompile(`\s+`)
	product_text, spd_marker := find_product_text(spd_text)
	re_spd := regexp.MustCompile(`(?ms)` + spd_marker + `:?\s+([A-Z0-9]{1,2}(?:\.|-)[A-Z0-9]{1,2}(?:\.|-)[A-Z0-9]{1,2})`)
	if len(product_text) == 0 {
		spd.Notes = spdFilename
	} else {
		spd.Notes = build_CDROM_identifier(last_path, pathToCDROM)
		raw_title := product_text
		if verbose {
			fmt.Println("Found text: [" + raw_title + "]")
		}
		spd_matches := re_spd.FindAllStringSubmatch(raw_title, -1)
		if len(spd_matches) == 1 {
			raw_spd := spd_matches[0][0]
			spd.ID = spd_matches[0][1]
			spd.Title = strings.TrimSpace(re_spaces.ReplaceAllLiteralString(strings.ReplaceAll(raw_title, raw_spd, ""), " "))
			// VAX EDI (UK) SPD contains this extra waring text in the title. Simply remove it unconditionally.
			spd.Title = strings.ReplaceAll(spd.Title, "This product is normally supplied in the U.K. only. For availability and conditions of supply in other countries, please contact the local DIGITAL office.", "")
			if verbose {
				fmt.Println("Found SPD: [" + spd.ID + "]")
				fmt.Println("Title:     [" + spd.Title + "]")
			}
		} else {
			if len(spd_matches) == 0 {
				fmt.Println("ISSUE: No SPD ID found")
			}
			if len(spd_matches) > 1 {
				fmt.Println("ISSUE: Too many SPD IDs found")
			}
		}

	}

	// Look for the SPD part number - this should occur only once in the file.
	// If the regexp matches more than once, then all is OK if all the matches are the same.
	re_part_num := regexp.MustCompile(`(?ms)^\s*([0-9A-Z]{2}-[0-9A-Z]{5}-[0-9A-Z]{2})\s*$`)
	part_num_matches := re_part_num.FindAllStringSubmatch(spd_text, -1)
	var part_num string
	part_num_valid := false
	if len(part_num_matches) == 0 {
		if verbose {
			fmt.Println("ISSUE: No part # found in", spdFilename)
		}
	} else {
		part_num = part_num_matches[0][1]
		part_num_valid = true
		for _, possible_part_num := range part_num_matches {
			if !strings.HasPrefix(possible_part_num[1], "Q") {
				if possible_part_num[1] != part_num {
					part_num_valid = false
					fmt.Println("ISSUE: Differing part # in", spdFilename, ",", part_num, "and", possible_part_num)
				}
			}
		}
		if part_num_valid {
			spd.Part_num = part_num
		}
	}

	// Look for a valid date
	re_date_standalone := regexp.MustCompile(`(?ms)^(?:\s*DIGITAL\s*)?([A-Z][A-Za-z]{1,})\s+((?:1|2)\d{3})\s*$`)
	spd_date_matches := re_date_standalone.FindAllStringSubmatch(spd_text, -1)
	spd_date_valid := false
	if len(spd_date_matches) == 0 {
		if verbose {
			fmt.Println("No date found in", spdFilename)
		}
	} else {
		spd_date_month := spd_date_matches[0][1]
		spd_date_year := spd_date_matches[0][2]
		spd_date_valid = true
		for _, possible_spd_date := range spd_date_matches {
			if (possible_spd_date[1] != spd_date_month) || (possible_spd_date[2] != spd_date_year) {
				spd_date_valid = false
				if verbose {
					fmt.Println("Differing SPD dates in", spdFilename, ",", spd_date_month, spd_date_year, "and", possible_spd_date[1], possible_spd_date[2])
				}
			}
		}
		if spd_date_valid {
			month_number_string := monthNamesToDigits[strings.ToLower(spd_date_month)]
			if month_number_string == "" {
				fmt.Println("ISSUE: Unrecognised month name: ["+spd_date_month+"] in", spdFilename)
			} else {
				spd.Date = spd_date_year + "-" + month_number_string
			}
		}
	}

	return spd
}

// Given a subdirectory name, use that to identify the CDROM in question and build a textual representation of that CDROM.
// For example, VAXVMS_CONOLD_1989_03_01 would be "VMS Consolidated Software Disc March 1989 Disc 1 of 1" which is close to what is printed on the physical CDROM label.
func build_CDROM_identifier(path string, pathToCDROM PathToCDROM) string {
	cdrom, ok := pathToCDROM[path]
	result := ""
	if !ok {
		fmt.Println("ISSUE: no CDROM for [" + path + "]")
	} else {
		result = cdrom.Title + " " + build_CDROM_month_year_date(cdrom.Date) + " Disc " + strconv.Itoa(cdrom.Number) + " of " + strconv.Itoa(cdrom.Total)
	}
	return result
}

// Find the title part of the SPD. This lies between two key phrases that depend on the SPD language.
// Also check that this text contains the keyword SPD (or its foreign language equivalent).
//
// Inputs:
//  spd_text: a string containing the full text of the SPD file
// Outputs:
//  string: the title text
//  string: the keyword used to identify the SPD
func find_product_text(spd_text string) (string, string) {

	provisional_match := ""

	// Match against an English language SPD
	re_text_eng := regexp.MustCompile(`(?msi)PRODUCT\s+NAME\s*:(.*?)DESCRIPTION`)
	matches := re_text_eng.FindAllStringSubmatch(spd_text, -1)
	if len(matches) > 0 {
		text := matches[0][1]
		if strings.Contains(text, "SPD") {
			return text, "SPD"
		} else {
			provisional_match = text
		}
	}

	// Match against a German SPD
	re_text_ger := regexp.MustCompile(`(?ims)PRODUKT(?:NAME)?\s*:(.*?)Beschreibung`)
	matches = re_text_ger.FindAllStringSubmatch(spd_text, -1)
	if len(matches) > 0 {
		text := matches[0][1]
		if strings.Contains(text, "SPD") {
			return text, "SPD"
		}
	}

	// Match against a Danish SPD
	re_text_dnk := regexp.MustCompile(`(?ms)PRODUKTNAVN\s*:(.*?)BESKRIVELSE`)
	matches = re_text_dnk.FindAllStringSubmatch(spd_text, -1)
	if len(matches) > 0 {
		text := matches[0][1]
		if strings.Contains(text, "SPD") {
			return text, "SPD"
		}
	}

	// Match against a Spanish SPD
	re_text_esp := regexp.MustCompile(`(?ms)NOMBRE\s+DEL\s+PRODUCTO\s*:(.*?)DESCRIPCI.N`)
	matches = re_text_esp.FindAllStringSubmatch(spd_text, -1)
	if len(matches) > 0 {
		text := matches[0][1]
		if strings.Contains(text, "SPD") {
			return text, "SPD"
		}
	}

	// Match against a French SPD
	re_text_fra := regexp.MustCompile(`(?ms)NOM\s+DU\s+PRODUIT\s*:(.*?)(?:1\s+)?DESCRIPTION`)
	matches = re_text_fra.FindAllStringSubmatch(spd_text, -1)
	if len(matches) > 0 {
		text := matches[0][1]
		// Some French SPDs refer to a DPL and others say SPD!
		if strings.Contains(text, "DPL") {
			return text, "DPL"
		}
		if strings.Contains(text, "SPD") {
			return text, "SPD"
		}
	}

	// Match against a Swedish SPD
	re_text_swe := regexp.MustCompile(`(?ms)PRODUKTNAMN\s*:(.*?)(?:ALLM.{1,3}N\s+)?BESKRIVNING`)
	matches = re_text_swe.FindAllStringSubmatch(spd_text, -1)
	if len(matches) > 0 {
		text := matches[0][1]
		if strings.Contains(text, "SPD") {
			return text, "SPD"
		}
	}

	// Match against a Finnish SPD
	re_text_fin := regexp.MustCompile(`(?ms)TUOTTEEN\s+NIMI\s*:(.*?)KUVAUS`)
	matches = re_text_fin.FindAllStringSubmatch(spd_text, -1)
	if len(matches) > 0 {
		text := matches[0][1]
		if strings.Contains(text, "SPD") {
			return text, "SPD"
		}
	}

	// Match against an Italian SPD
	re_text_ita := regexp.MustCompile(`(?ms)NOME\s+(?:DEL\s+)?PRODOTTO\s*:(.*?)DESCRIZIONE`)
	matches = re_text_ita.FindAllStringSubmatch(spd_text, -1)
	if len(matches) > 0 {
		text := matches[0][1]
		if strings.Contains(text, "SPD") {
			return text, "SPD"
		}
	}

	// Match against a Dutch SPD
	re_text_nld := regexp.MustCompile(`(?ms)PRODUKTNAAM\s*:(.*?)BESCHRIJVING`)
	matches = re_text_nld.FindAllStringSubmatch(spd_text, -1)
	if len(matches) > 0 {
		text := matches[0][1]
		if strings.Contains(text, "SPD") {
			return text, "SPD"
		}
	}

	// Match against an English language SPD that does not say "PRODUCT NAME:"
	re_text_eng_leadless := regexp.MustCompile(`(?ms)\A\s*(.*?)^\s*(?:PRODUCT\s+)?DESCRIPTION\s*$`)
	matches = re_text_eng_leadless.FindAllStringSubmatch(spd_text, -1)
	if len(matches) > 0 {
		text := matches[0][1]
		if strings.Contains(text, "SPD") {
			return text, "SPD"
		}
	}

	return provisional_match, "SPD"
}

// Construct a map of "month" => "two digit number string"
// As SPDs in new languages will keep coming along, use YAML to store this externally.
func build_month_names_to_digits_map(monthsYAMLFilename string) MonthNameToNumber {
	// Read in the data that maps (lowercase) month name to a two digit, leading 0, month number
	monthsYAMLData, err := ioutil.ReadFile(monthsYAMLFilename)
	if err != nil {
		log.Fatal(err)
	}

	var monthsData MonthNameToNumber
	err = yaml.Unmarshal(monthsYAMLData, &monthsData)
	if err != nil {
		log.Fatal(err)
	}

	return monthsData
}

// Construct a map of "path" => CDROM object
// This will be used to convert an SPD file subdirectory into identification for the CDROM
func build_path_to_CDROM_map(cdrom_yaml_file string) PathToCDROM {
	// Read in the data that identifies a CDROM from the path
	cdroms_yaml_data, err := ioutil.ReadFile(cdrom_yaml_file)
	if err != nil {
		log.Fatal(err)
	}

	var cdrom_data []CDROM
	err = yaml.Unmarshal(cdroms_yaml_data, &cdrom_data)
	if err != nil {
		log.Fatal(err)
	}

	result := make(PathToCDROM)
	for _, v := range cdrom_data {
		result[v.Path] = v
	}

	return result
}

// Takes a date in YYYY-MM format and turns it into the English month name and the year.
// So "1999-06" becomes "June 1996".
// This is to match the date printed on the OpenVMS VAX discs of the late 1990s
func build_CDROM_month_year_date(yyyy_mm string) string {
	ym := strings.Split(yyyy_mm, "-")
	month, _ := strconv.Atoi(ym[1])
	m := time.Month(month)
	return m.String() + " " + ym[0]
}

// Replace multiple spaces, newline and CR with a single space
func TrimSpacesAndNewLines(s string) string {
	re := regexp.MustCompile(` +|\r|\n`)
	return re.ReplaceAllString(s, " ")
}
