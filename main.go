package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
)

const lastCheckTimeFilePath = "./last-check.txt"

func updateLastCheckTimeFile(now time.Time) {
	timeFile, err := os.Create(lastCheckTimeFilePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error creating file:", err)
		os.Exit(1)
	}
	defer timeFile.Close()
	_, err = timeFile.WriteString(now.Format(time.RFC3339))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error writing to file:", err)
		os.Exit(1)
	}
}

func getLastCheckTime() time.Time {
	var lastCheckTime time.Time

	if _, err := os.Stat(lastCheckTimeFilePath); os.IsNotExist(err) {
		lastCheckTime = time.Date(1, 1, 1, 0, 0, 0, 0, time.Local)
	} else {
		bytes, err := os.ReadFile(lastCheckTimeFilePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error reading file:", err)
			os.Exit(1)
		}
		parsed, err := time.Parse(time.RFC3339, string(bytes))
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error parsing time:", err)
			os.Exit(1)
		}
		lastCheckTime = parsed
	}

	return lastCheckTime
}

func readFeedUrls() map[string]string {
	const filePath = "../../feeds.json"

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error opening file:", err)
		os.Exit(1)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var data map[string]string
	err = decoder.Decode(&data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error decoding JSON:", err)
		os.Exit(1)
	}
	return data
}

func requestFeed(url string) string {
	res, err := http.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error getting feed:", err)
		return ""
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading body:", err)
		return ""
	}

	return string(body)
}

func main() {
	feedsMap := readFeedUrls()

	now := time.Now()
	lastCheckTime := getLastCheckTime()
	// TODO: remove — for testing only
	// lastCheckTime = time.Date(2023, 6, 1, 0, 0, 0, 0, time.Local)

	// write current time to timeFile
	updateLastCheckTimeFile(now)

	var wg sync.WaitGroup

	// create a buffered channel (which acts as a semaphone)
	// to control concurrency
	maxConcurrency := 2
	// struct{} is an empty struct, which takes up no memory
	sem := make(chan struct{}, maxConcurrency)

	var results = make(map[string][]*gofeed.Item)

	for name, url := range feedsMap {
		wg.Add(1)
		go func(name string, url string) {
			sem <- struct{}{} // blocks if channel is full
			defer wg.Done()

			// fmt.Println(name)
			content := requestFeed(url)

			// parse feed
			parser := gofeed.NewParser()
			feed, err := parser.ParseString(content)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error parsing feed:", err)
				return
			}

			filtered := make([]*gofeed.Item, 0)

			for _, item := range feed.Items {
				if item.PublishedParsed.After(lastCheckTime) {
					filtered = append(filtered, item)
				}
			}

			results[name] = filtered

			<-sem // release
		}(name, url)
	}

	wg.Wait()

	newItemsCount := 0
	for name, items := range results {
		c := len(items)
		if c == 0 {
			continue
		}

		newItemsCount += c
		fmt.Println()
		fmt.Println(name + ": " + fmt.Sprint(c))

		// reverse sort by date
		reversedItems := items[:] // copy
		sort.Slice(reversedItems, func(i, j int) bool {
			a := *reversedItems[i].PublishedParsed
			b := *reversedItems[j].PublishedParsed
			return a.After(b)
		})

		for _, item := range reversedItems {
			timestamp := item.PublishedParsed.Format(time.RFC3339)
			timestamp = strings.Split(timestamp, "T")[0]
			fmt.Println(
				"-",
				fmt.Sprintf("(%s)", timestamp),
				item.Title,
			)
		}
	}

	if newItemsCount == 0 {
		fmt.Println("No new items")
	}
}
