package sqlc

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var debugLogging = os.Getenv("SQLC_DEBUG") == "true"

// ShouldIgnoreMapping checks if a model-to-insert/update mapping should be ignored
// Simple rule: if the function name (minus "Params" and "ToInsert/ToUpdate") matches an ignored query, skip it
func ShouldIgnoreMapping(ignoredQueries map[string]bool, modelName, paramsStructName string) bool {
	// For insert functions: ModelNameToInsertParams -> ModelName
	// Check if ModelName matches any ignored query
	if strings.Contains(paramsStructName, "Insert") && strings.Contains(paramsStructName, "Params") {
		if ignoredQueries[modelName] {
			return true
		}
	}

	// For update functions: ModelNameToUpdateParams -> ModelName
	// Check if ModelName matches any ignored query
	if strings.Contains(paramsStructName, "Update") && strings.Contains(paramsStructName, "Params") {
		if ignoredQueries[modelName] {
			return true
		}
	}

	// Also check the direct params struct name
	if ignoredQueries[paramsStructName] {
		return true
	}

	return false
}

// GetIgnoredQueries parses SQL files and returns a set of query names to ignore
// Queries can be ignored by adding a comment: -- sqlc:ignore-generation
// Returns both the original query name and the Params variant for insert/update queries
func GetIgnoredQueries(directory string) map[string]bool {
	ignored := make(map[string]bool)

	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".sql") {
			file, err := os.Open(path)
			if err != nil {
				return nil // Skip files that can't be opened
			}
			defer file.Close()

			scanner := bufio.NewScanner(file)
			var currentQuery string
			ignoreCurrent := false

			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())

				if strings.HasPrefix(line, "-- name:") {
					// Save the previous query if it was marked for ignoring
					if currentQuery != "" && ignoreCurrent {
						ignored[currentQuery] = true
						// Also add the Params variant for insert/update queries
						if strings.Contains(currentQuery, "Insert") || strings.Contains(currentQuery, "Update") {
							ignored[currentQuery+"Params"] = true
						}
						if debugLogging {
							log.Printf("Ignoring query: %s", currentQuery)
						}
					}

					// Extract query name from "-- name: QueryName :type"
					parts := strings.Fields(line)
					if len(parts) >= 3 {
						currentQuery = parts[2]
						ignoreCurrent = false
					}
				} else if strings.Contains(line, "-- sqlc:ignore-generation") {
					// Mark current query for ignoring
					ignoreCurrent = true
				}
			}

			// Handle the last query in the file
			if currentQuery != "" && ignoreCurrent {
				ignored[currentQuery] = true
				// Also add the Params variant for insert/update queries
				if strings.Contains(currentQuery, "Insert") || strings.Contains(currentQuery, "Update") {
					ignored[currentQuery+"Params"] = true
				}
				if debugLogging {
					log.Printf("Ignoring query: %s", currentQuery)
				}
			}
		}
		return nil
	})

	if err != nil && debugLogging {
		log.Printf("Error walking SQL directory: %v", err)
	}

	return ignored
}
