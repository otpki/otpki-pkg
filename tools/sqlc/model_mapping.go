package sqlc

import (
	"math"
	"regexp"
	"slices"
	"strings"
)

type ModelMatch struct {
	ModelName string
	Score     float64
}

type MappingRegistry struct {
	Models []string
	// Noise words that should not influence the identity of the struct
	Noise map[string]bool
}

func NewMappingRegistry(models []string) *MappingRegistry {
	sortedModels := slices.Clone(models)
	slices.Sort(sortedModels)
	return &MappingRegistry{
		Models: sortedModels,
		Noise: map[string]bool{
			"update": true, "create": true, "insert": true, "delete": true, "get": true,
			"list": true, "params": true, "request": false, "response": true,
			"mask": true, "with": true, "patch": true,
		},
	}
}

func (r *MappingRegistry) FindBestMatch(paramName string) (string, float64) {
	paramIdentity := r.getCoreIdentity(paramName)
	if len(paramIdentity) == 0 {
		return "", 0.0
	}

	var bestModel string
	var highValue float64

	for _, modelName := range r.Models {
		modelIdentity := r.getCoreIdentity(modelName)
		score := r.calculateScore(modelIdentity, paramIdentity)

		if score > highValue {
			highValue = score
			bestModel = modelName
		}
	}

	return bestModel, highValue
}

func (r *MappingRegistry) calculateScore(modelID, paramID []string) float64 {
	matches := 0
	for _, m := range modelID {
		for _, p := range paramID {
			// Check for exact or plural match
			if m == p || strings.TrimSuffix(p, "s") == m || strings.TrimSuffix(m, "s") == p {
				matches++
				break
			}
		}
	}

	if matches == 0 {
		return 0.0
	}

	// Basic coverage: how many model tokens did we find?
	coverage := float64(matches) / float64(len(modelID))

	// Penalty: If the param has extra nouns (like junction tables),
	// reduce the score significantly.
	penalty := 1.0
	if len(paramID) > len(modelID) {
		// Reduce score by 20% for every extra token found
		diff := len(paramID) - len(modelID)
		penalty = 1.0 - (float64(diff) * 0.2)
	}

	// if MODEL name contains multiple distinct entity names
	// that don't appear together in the param name, apply additional penalty
	isJunction := r.isJunctionTable(modelID, paramID)
	if isJunction {
		penalty *= 0.75 // additional 25% penalty for likely junction tables
	}

	finalScore := coverage * penalty
	return math.Max(0, finalScore)
}

func (r *MappingRegistry) isJunctionTable(modelID, paramID []string) bool {
	// check if this looks like a concatenation of multiple entity names
	// For junction tables, we typically see patterns like:
	// EndEntityProfile + CertificateProfile = EndEntityProfileCertificateProfile

	// try to find splits that represent complete entity names
	for i := 1; i < len(modelID); i++ {
		firstEntity := modelID[:i]
		secondEntity := modelID[i:]

		// check if both parts could be complete entity names
		if r.couldBeEntityName(firstEntity) && r.couldBeEntityName(secondEntity) {
			return true
		}
	}

	return false
}

func (r *MappingRegistry) couldBeEntityName(tokens []string) bool {
	// check if these tokens match any known model
	for _, model := range r.Models {
		modelTokens := r.getCoreIdentity(model)
		if len(tokens) >= len(modelTokens) && r.subsetMatch(tokens, modelTokens) {
			return true
		}
	}
	return false
}

// subsetMatch checks if all modelTokens appear in tokens in order
func (r *MappingRegistry) subsetMatch(tokens, modelTokens []string) bool {
	if len(modelTokens) == 0 {
		return true
	}

	tokenIndex := 0
	for _, modelToken := range modelTokens {
		found := false
		for i := tokenIndex; i < len(tokens); i++ {
			if tokens[i] == modelToken {
				tokenIndex = i + 1
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (r *MappingRegistry) getCoreIdentity(s string) []string {
	allTokens := tokenize(s)
	var core []string
	for _, t := range allTokens {
		if !r.Noise[t] {
			core = append(core, t)
		}
	}
	return core
}

func tokenize(s string) []string {
	// 1. nsert a space before any uppercase letter that is followed by a lowercase letter
	// 2. Insert a space after any lowercase letter that is followed by an uppercase letter
	// This handles the "TLSCert" -> "TLS Cert" transition.

	// Look for: [Upper][Upper][Lower] -> Insert space between U1 and U2
	reAcronym := regexp.MustCompile(`([A-Z])([A-Z][a-z])`)
	s = reAcronym.ReplaceAllString(s, `$1 $2`)

	// Look for: [Lower][Upper] -> Insert space between L and U
	reWord := regexp.MustCompile(`([a-z0-9])([A-Z])`)
	s = reWord.ReplaceAllString(s, `$1 $2`)

	// Split by whitespace and normalize
	parts := strings.Fields(s)
	var tokens []string
	for _, p := range parts {
		tokens = append(tokens, normalize(strings.ToLower(p)))
	}
	return tokens
}

func normalize(token string) string {
	token = strings.ToLower(token)
	// Very basic plural normalization
	if len(token) > 3 && strings.HasSuffix(token, "s") {
		return strings.TrimSuffix(token, "s")
	}
	return token
}
