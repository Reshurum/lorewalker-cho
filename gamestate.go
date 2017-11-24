// Discord bot that does World of Warcraft trivia.
// Copyright (C) 2017  Walter Kuppens
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/texttheater/golang-levenshtein/levenshtein"

	"github.com/go-redis/redis"
	"github.com/vmihailenco/msgpack"
)

// GameState represents a game context taking place in a server.
type GameState struct {
	Revision           int
	StartTime          time.Time
	Question           string
	Answers            []string
	RemainingQuestions int
	LastQuestionIndex  int
	Started            bool
	Finished           bool
	Waiting            bool
	GuildID            string
	ChannelID          string
	UserScores         map[string]int
}

// Winner is a Discord user id with an associated score.
type Winner struct {
	UserID string
	Score  int
}

// Question represents a question and valid answers associated with it.
type Question struct {
	QuestionText string   `json:"questionText"`
	Answers      []string `json:"answers"`
}

// gameStateRevision is the current version of the GameState struct. If this
// structure changes we need a way to detect old versions of the structure so we
// can convert them to the newer version.
const gameStateRevision = 1

// gameStateNamespace is prepended to keys relating to active games.
const gameStateNamespace = "chotrivia.games"

// gameStateLifetime is the amount of time a GameState object will persist.
const gameStateLifetime = 86400 * time.Second

// Save serializes a GameState struct using msgpack and stores it in the redis
// cluster with a lifetime of a day.
func (gs *GameState) Save(rcli *redis.Client) error {
	var (
		err error
	)

	data, err := msgpack.Marshal(gs)
	if err != nil {
		return err
	}

	key := BuildGameStateKey(gs.GuildID)
	err = rcli.Set(key, data, gameStateLifetime).Err()
	if err != nil {
		return err
	}

	return nil
}

// GetWinners returns a list of users with the highest score. A list is returned
// as multiple users can tie for a high score.
func (gs *GameState) GetWinners() []Winner {
	// Find out what the highest score is.
	highestScore := 0
	for _, v := range gs.UserScores {
		if v > highestScore {
			highestScore = v
		}
	}

	// There are no winners if no one tries :\
	if highestScore <= 0 {
		return []Winner{}
	}

	// Find users with the highest score, more than 1 means a tie.
	winners := []Winner{}
	for k, v := range gs.UserScores {
		if v == highestScore {
			winners = append(winners, Winner{
				UserID: k,
				Score:  v,
			})
		}
	}

	return winners
}

// ChooseRandomQuestion returns a random question from the questions list.
func (gs *GameState) ChooseRandomQuestion() {
	index := rand.Intn(len(choConfig.Questions))
	if index == gs.LastQuestionIndex {
		index++
	}
	if index >= len(choConfig.Questions) {
		index = 0
	}

	question := choConfig.Questions[index]
	gs.LastQuestionIndex = index
	gs.Question = question.QuestionText
	gs.Answers = question.Answers
}

// CheckAnswer checks a user-provided answer using Levenshtein Distance.
func (gs *GameState) CheckAnswer(answer string) bool {
	var bestRatio float64
	for _, correctAnswer := range gs.Answers {
		ratio := levenshtein.RatioForStrings(
			[]rune(strings.ToLower(correctAnswer)),
			[]rune(strings.ToLower(answer)),
			levenshtein.DefaultOptions)
		if bestRatio < ratio {
			bestRatio = ratio
		}
	}
	return bestRatio >= choConfig.Ratio
}

// CreateGameState initializes a GameState struct with default values.
func CreateGameState(guildID string, channelID string) *GameState {
	gs := &GameState{
		Revision:           gameStateRevision,
		StartTime:          time.Now().UTC(),
		Question:           "Which mod doesn't give me questions to ask?",
		Answers:            []string{},
		LastQuestionIndex:  -1,
		RemainingQuestions: choConfig.QuestionCount,
		Started:            false,
		Finished:           false,
		Waiting:            false,
		GuildID:            guildID,
		ChannelID:          channelID,
		UserScores:         make(map[string]int),
	}
	gs.ChooseRandomQuestion()
	return gs
}

// LoadGameState queries for a GameState struct in redis and deserializes it. If
// there's no game object in redis a new one is created and returned.
func LoadGameState(rcli *redis.Client, guildID string, channelID string) (*GameState, error) {
	var (
		err error
	)

	key := BuildGameStateKey(guildID)
	data, err := rcli.Get(key).Bytes()
	if err == redis.Nil && len(channelID) > 0 {
		return CreateGameState(guildID, channelID), nil
	} else if err != nil {
		return nil, err
	}

	gs := &GameState{}
	err = msgpack.Unmarshal(data, gs)
	if err != nil {
		return nil, err
	}

	return gs, nil
}

// BuildGameStateKey returns a key to a specific GameState stored in redis.
func BuildGameStateKey(guildID string) string {
	return fmt.Sprintf("%s.%s", gameStateNamespace, guildID)
}
