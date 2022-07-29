package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/natefinch/lumberjack"
)

type FoundFile struct {
	FilePath string
	Keywords []string
}

type NumFiles struct {
	FoundFiles    uint64
	SearchedFiles uint64
	NumErrors     uint64
	NumIgnored    uint64
}

func FileCollector(filesChan chan *FoundFile, outFile string) {
	f, err := os.OpenFile(outFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	for filePath := range filesChan {
		fileString := fmt.Sprintf("Path: %s | Keywords: %v\n", filePath.FilePath, strings.Join(filePath.Keywords, " & "))
		log.Print(fileString)
		// Append to output file
		f.WriteString(fileString)
	}
	doneMsg := "Finished collecting output files\n"
	log.Printf(doneMsg)
	// fmt.Printf(doneMsg)
}

func ErrorCollector(errorChan chan string, outFile string, numErrors *uint64) {
	f, err := os.OpenFile(outFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	for fileErrorPath := range errorChan {
		atomic.AddUint64(numErrors, 1)
		log.Printf("Error occured for file: %s\n", fileErrorPath)
		// Append to output file
		f.WriteString(fileErrorPath + "\n")
	}
	doneMsg := "Finished collecting error files\n"
	log.Printf(doneMsg)
	// fmt.Printf(doneMsg)
}

func NewThreadSearchFile(filePath string, kws []string, regexs []*regexp.Regexp, fileChan chan *FoundFile, errChan chan string, searchedFiles *uint64, wg *sync.WaitGroup) {
	defer func() {
		if r := recover(); r != nil {
			// fmt.Println("Something went wrong!", r)
			errChan <- fmt.Sprintf("%s = %v", filePath, r)
			// fmt.Printf("Error occured for file: %s\n", filePath)
			return
		}
	}()
	defer wg.Done()
	f, err := os.OpenFile(filePath, os.O_RDONLY, 0755)
	if err != nil {
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			// fmt.Println("Error occured for file (permission denied):", filePath)
			errChan <- fmt.Sprintf("%s = %v", filePath, err)
			return
		}
		errChan <- fmt.Sprintf("%s = %v", filePath, err)
		// fmt.Printf("Error occured for file: %s\n%v", filePath, err)
		return
	}
	defer f.Close()

	searchMsg := fmt.Sprintf("Searching File: %s\n", filePath)
	// fmt.Print(searchMsg)
	log.Print(searchMsg)

	// Splits on newlines by default.
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	file := new(FoundFile)
	cleanFP := strings.TrimSpace(filePath)
	file.FilePath = cleanFP
	file.Keywords = make([]string, 0)

	line := 1
	hit := false
	for scanner.Scan() {
		lineText := scanner.Text()
		for _, kw := range kws {
			if strings.Contains(lineText, kw) {
				kwLine := fmt.Sprintf("%s::%d", kw, line)
				log.Printf("Found keyword in: %s (KW=%s)\n", cleanFP, kwLine)
				file.Keywords = append(file.Keywords, kwLine)
				hit = true
			}
		}
		for _, regex := range regexs {
			if match := regex.FindString(lineText); match != "" {
				reLine := fmt.Sprintf("%s:%s:%d", match, regex.String(), line)
				log.Printf("Found regex in: %s (RE=%s|STR=%s)\n", cleanFP, reLine, match)
				file.Keywords = append(file.Keywords, reLine)
				hit = true
			}
		}
		line++
	}

	atomic.AddUint64(searchedFiles, 1)

	err = scanner.Err()
	if err == bufio.ErrTooLong {
		errChan <- fmt.Sprintf("%s = %v", filePath, err)
		// fmt.Printf("Error occured for file (line too long): %s\n", filePath)
		return
	} else if err != nil {
		errChan <- fmt.Sprintf("%s = %v", filePath, err)
		// fmt.Printf("Error occured for file: %s\n%v\n%v", filePath, err)
		return
	}

	if hit {
		fileChan <- file
	}
}

func NewThreadFileFinder(directory string, keywords []string, regexs []*regexp.Regexp, ignored_types []string, outputChan chan *FoundFile, errChan chan string, fileCounter *NumFiles, wg *sync.WaitGroup) {
	defer wg.Done()
	walkFunc := func(path string, dir fs.DirEntry, err error) error {
		log.Printf("Found file: %s | Err: %v\n", path, err)
		if err != nil {
			// fmt.Printf("Error occured for file: %s\n%v", path, err)
			errChan <- fmt.Sprintf("%s = %v", path, err)
			return nil
		}
		if dir.IsDir() {
			return nil
		}
		for _, ignore := range ignored_types {
			if strings.Contains(strings.ToLower(path), strings.ToLower(ignore)) {
				log.Printf("Ignoring file: %s due to ignored string (%s)\n", path, ignore)
				return nil
			}
		}
		// fmt.Printf("Found File: %s\n", path)
		atomic.AddUint64(&fileCounter.FoundFiles, 1)
		wg.Add(1)
		go NewThreadSearchFile(path, keywords, regexs, outputChan, errChan, &fileCounter.SearchedFiles, wg)
		return nil
	}
	err := filepath.WalkDir(directory, walkFunc)
	if err != nil {
		errMsg := fmt.Sprintf("Error occured for directory: %s\n%v", directory, err)
		// fmt.Print(errMsg)
		log.Print(errMsg)
		return
	}
}

func SameThreadSearchFile(kws []string, regexs []*regexp.Regexp, fileChan, outputChan chan *FoundFile, errChan chan string, searchedFiles *uint64, wg *sync.WaitGroup) {
	defer wg.Done()
	for file := range fileChan {
		func() {
			filePath := file.FilePath
			defer func() {
				if r := recover(); r != nil {
					// fmt.Println("Something went wrong!", r)
					errChan <- fmt.Sprintf("%s = %v", filePath, r)
					// fmt.Printf("Error occured for file: %s\n", filePath)
					return
				}
			}()
			f, err := os.OpenFile(filePath, os.O_RDONLY, 0755)
			if err != nil {
				var pathError *os.PathError
				if errors.As(err, &pathError) {
					// fmt.Println("Error occured for file (permission denied):", filePath)
					errChan <- fmt.Sprintf("%s = %v", filePath, err)
					return
				}
				errChan <- fmt.Sprintf("%s = %v", filePath, err)
				// fmt.Printf("Error occured for file: %s\n%v", filePath, err)
				return
			}
			defer f.Close()

			searchMsg := fmt.Sprintf("Searching File: %s\n", filePath)
			// fmt.Print(searchMsg)
			log.Print(searchMsg)

			// Splits on newlines by default.
			scanner := bufio.NewScanner(f)
			buf := make([]byte, 0, 64*1024)
			scanner.Buffer(buf, 1024*1024)

			line := 1
			hit := false
			for scanner.Scan() {
				lineText := scanner.Text()
				for _, kw := range kws {
					if strings.Contains(lineText, kw) {
						kwLine := fmt.Sprintf("%s:%d", kw, line)
						// fmt.Printf("Found keyword in: %s (KW=%s)\n", filePath, kwLine)
						file.Keywords = append(file.Keywords, kwLine)
						hit = true
					}
				}
				for _, regex := range regexs {
					if match := regex.FindString(lineText); match != "" {
						reLine := fmt.Sprintf("%s:%s:%d", match, regex.String(), line)
						log.Printf("Found regex in: %s (RE=%s|STR=%s)\n", filePath, regex.String(), reLine)
						file.Keywords = append(file.Keywords, reLine)
						hit = true
					}
				}
				line++
			}

			atomic.AddUint64(searchedFiles, 1)

			err = scanner.Err()
			if err == bufio.ErrTooLong {
				errChan <- fmt.Sprintf("%s = %v", filePath, err)
				// fmt.Printf("Error occured for file (line too long): %s\n", filePath)
				return
			} else if err != nil {
				errChan <- fmt.Sprintf("%s = %v", filePath, err)
				// fmt.Printf("Error occured for file: %s\n%v\n%v", filePath, err)
				return
			}

			if hit {
				outputChan <- file
			}
		}()
	}
}

func SameThreadFileFinder(directory string, ignored_types []string, fileChan chan *FoundFile, errChan chan string, fileCounter *NumFiles) {
	walkFunc := func(path string, dir fs.DirEntry, err error) error {
		log.Printf("Found file: %s | Err: %v\n", path, err)
		if err != nil {
			// fmt.Printf("Error occured for file: %s\n%v", path, err)
			errChan <- fmt.Sprintf("%s = %v", path, err)
			return nil
		}
		if dir.IsDir() {
			return nil
		}
		for _, ignore := range ignored_types {
			if strings.Contains(strings.ToLower(path), strings.ToLower(ignore)) {
				log.Printf("Ignoring file: %s due to ignored string (%s)\n", path, ignore)
				return nil
			}
		}
		// fmt.Printf("Found File: %s\n", path)
		atomic.AddUint64(&fileCounter.FoundFiles, 1)
		fileChan <- &FoundFile{FilePath: path, Keywords: make([]string, 0)}
		return nil
	}
	err := filepath.WalkDir(directory, walkFunc)
	if err != nil {
		errMsg := fmt.Sprintf("Error occured for directory: %s\n%v", directory, err)
		// fmt.Print(errMsg)
		log.Print(errMsg)
		return
	}
	doneMsg := fmt.Sprintf("Finished finding files (found #%d files) through directory: %s\n", fileCounter.FoundFiles, directory)
	log.Printf(doneMsg)
	// fmt.Printf(doneMsg)
	close(fileChan)
}

func main() {

	// Create log to file
	log.SetOutput(&lumberjack.Logger{
		Filename:   "log.txt",
		MaxSize:    1024, // megabytes
		MaxBackups: 5,
		Compress:   true,
	})

	start := time.Now()
	log.Printf("Starting program at: %s\n", start.Format(time.RFC3339))

	// get command line args
	var (
		outputPath       string
		errPath          string
		keywordsPath     string
		regexPath        string
		directoryPath    string
		ignoredPath      string
		ignoredTypesPath string
		threadCount      int
	)
	var newThread bool = false
	args := os.Args[1:]
	for _, arg := range args {
		if strings.Contains(arg, "directory=") {
			directoryPath = strings.Split(arg, "=")[1]
		} else if strings.Contains(arg, "ignore=") {
			ignoredPath = strings.Split(arg, "=")[1]
		} else if strings.Contains(arg, "ignoretypes=") {
			ignoredTypesPath = strings.Split(arg, "=")[1]
		} else if strings.Contains(arg, "keywords=") {
			keywordsPath = strings.Split(arg, "=")[1]
		} else if strings.Contains(arg, "regex=") {
			regexPath = strings.Split(arg, "=")[1]
		} else if strings.Contains(arg, "output=") {
			outputPath = strings.Split(arg, "=")[1]
		} else if strings.Contains(arg, "error=") {
			errPath = strings.Split(arg, "=")[1]
		} else if strings.Contains(arg, "newthread=") {
			switch strings.ToLower(strings.Split(arg, "=")[1]) {
			case "true":
				newThread = true
			case "false":
				newThread = false
			default:
				fmt.Println("Invalid value for newthread. Must be 'true' or 'false' exactly.")
				return
			}
		} else if strings.Contains(arg, "threadcount=") {
			customThreadCount, err := strconv.Atoi(strings.Split(arg, "=")[1])
			if err != nil {
				fmt.Println("Invalid value for threadcount. Must be an integer.")
				return
			}
			threadCount = customThreadCount
		} else {
			fmt.Println("Unknown argument:", arg)
		}
	}

	if directoryPath == "" {
		fmt.Printf("Directory must be given.")
		fmt.Print("Enter the directory to search for files:\n> ")
		fmt.Scanln(&directoryPath)
		if stat, _ := os.Stat(directoryPath); stat == nil {
			fmt.Printf("Directory does not exist: %s\n", directoryPath)
			return
		}
	}
	if keywordsPath == "" {
		keywordsPath = "keywords.txt"
	}
	if regexPath == "" {
		regexPath = "regex.txt"
	}
	if outputPath == "" {
		outputPath = "output.txt"
	}
	if errPath == "" {
		errPath = "error.txt"
	}
	if ignoredPath == "" {
		ignoredPath = "ignore.txt"
	}
	if ignoredTypesPath == "" {
		ignoredTypesPath = "ignore-types.txt"
	}
	if threadCount == 0 {
		threadCount = runtime.NumCPU()
	}

	log.Printf("Using options:\n\tDirectory: %s\n\tKeywords: %s\n\tOutput: %s\n\tErrors: %s\n\tIgnored: %s\n\tIgnored Types: %s\n\tNew Thread: %t\n", directoryPath, keywordsPath, outputPath, errPath, ignoredPath, ignoredTypesPath, newThread)

	// Get keywords to search for
	// Check if keywords file exists
	if _, err := os.Stat(keywordsPath); errors.Is(err, os.ErrNotExist) {
		panic(fmt.Errorf("error: keywords file does not exist: %s\n", keywordsPath))
	}
	kwf, err := os.Open(keywordsPath)
	if err != nil {
		panic(err)
	}
	defer kwf.Close()
	// compile the lines into a slice of strings
	scanner := bufio.NewScanner(kwf)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	var keywords []string
	for scanner.Scan() {
		cleanKW := strings.TrimSpace(scanner.Text())
		if cleanKW != "" {
			keywords = append(keywords, cleanKW)
		}
	}
	// Get Regexes to search for
	// Check if Regex file exists
	if _, err := os.Stat(regexPath); errors.Is(err, os.ErrNotExist) {
		panic(fmt.Errorf("error: regex file does not exist: %s\n", regexPath))
	}
	ref, err := os.Open(regexPath)
	if err != nil {
		panic(err)
	}
	defer ref.Close()
	re_scanner := bufio.NewScanner(ref)
	re_buf := make([]byte, 0, 64*1024)
	re_scanner.Buffer(re_buf, 1024*1024)
	var regexs []*regexp.Regexp
	for re_scanner.Scan() {
		cleanRE := strings.TrimSpace(re_scanner.Text())
		if cleanRE != "" {
			regex, err := regexp.Compile(cleanRE)
			if err != nil {
				fmt.Printf("Regex (%s) failed to compile, this regex will not be searched: %s\n", cleanRE, err)
				log.Printf("Regex (%s) failed to compile, this regex will not be searched: %s\n", cleanRE, err)
			}
			regexs = append(regexs, regex)
		}
	}

	// Get ignored directories
	// Check if ignored file exists
	if _, err := os.Stat(ignoredPath); errors.Is(err, os.ErrNotExist) {
		panic(fmt.Errorf("error: ignored file does not exist: %s\n", ignoredPath))
	}
	igf, err := os.Open(ignoredPath)
	if err != nil {
		panic(err)
	}
	defer igf.Close()
	// compile the lines into a slice of strings
	scanner = bufio.NewScanner(igf)
	buf = make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	var ignored_strings []string
	for scanner.Scan() {
		cleanDIR := strings.TrimSpace(scanner.Text())
		if cleanDIR != "" {
			ignored_strings = append(ignored_strings, cleanDIR)
		}
	}
	// Get ignored types
	// Check if ignored file exists
	if _, err := os.Stat(ignoredTypesPath); errors.Is(err, os.ErrNotExist) {
		panic(fmt.Errorf("error: ignored types file does not exist: %s\n", ignoredTypesPath))
	}
	igtf, err := os.Open(ignoredTypesPath)
	if err != nil {
		panic(err)
	}
	defer igtf.Close()
	// compile the lines into a slice of strings
	scanner = bufio.NewScanner(igtf)
	buf = make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		cleanDIR := strings.TrimSpace(scanner.Text())
		if cleanDIR != "" {
			ignored_strings = append(ignored_strings, cleanDIR)
		}
	}

	fileCounter := &NumFiles{FoundFiles: 0, SearchedFiles: 0}

	wg := &sync.WaitGroup{}
	outputChan := make(chan *FoundFile)
	errChan := make(chan string)
	doneChan := make(chan bool)
	doneFunc := func() {
		for done := range doneChan {
			if done {
				break
			}
			for {
				fmt.Printf("\rFound files: %d | Searched files: %d | Files with errors: %d | Files ignored: %d | Elapsed time: %s", fileCounter.FoundFiles, fileCounter.SearchedFiles, fileCounter.NumErrors, fileCounter.NumIgnored, time.Since(start))
				time.Sleep(time.Second / 4)
			}
		}
	}

	if newThread {
		wg.Add(1)
		go FileCollector(outputChan, outputPath)
		go ErrorCollector(errChan, errPath, &fileCounter.NumErrors)
		go NewThreadFileFinder(directoryPath, keywords, regexs, ignored_strings, outputChan, errChan, fileCounter, wg)
		go doneFunc()
		doneChan <- false
		wg.Wait()
		close(doneChan)
		close(outputChan)
		close(errChan)
	} else {
		fileChan := make(chan *FoundFile)
		go FileCollector(outputChan, outputPath)
		go ErrorCollector(errChan, errPath, &fileCounter.NumErrors)
		threadMessage := fmt.Sprintf("Starting %d threads\n", threadCount)
		fmt.Print(threadMessage)
		log.Print(threadMessage)
		for i := 0; i < threadCount; i++ {
			wg.Add(1)
			go SameThreadSearchFile(keywords, regexs, fileChan, outputChan, errChan, &fileCounter.SearchedFiles, wg)
		}
		go doneFunc()
		doneChan <- false
		SameThreadFileFinder(directoryPath, ignored_strings, fileChan, errChan, fileCounter)
		wg.Wait()
		close(doneChan)
		close(outputChan)
		close(errChan)
	}

	filesFound := fmt.Sprintf("\nFound/Searched Files: %d/%d", fileCounter.FoundFiles, fileCounter.SearchedFiles)
	fmt.Printf(filesFound + "\n")
	elapsedTime := fmt.Sprintf("Time elapsed: %s", time.Since(start))
	fmt.Printf(elapsedTime + "\n")
	log.Println(filesFound, "|", elapsedTime)
	fmt.Printf("\nFile search completed, view found files in '%s', files which could not be searched in '%s', and a log of all files searched in 'log.txt'\n", outputPath, errPath)
}
