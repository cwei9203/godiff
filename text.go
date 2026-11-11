package main

import "github.com/egoist/godiff/locales"

// msg returns the catalogs' messages in the language the interface shows.
//
// The review's words are written once and in one place. Those the language
// pass has reached come from the catalogs in locales/, and the ones it has not
// reached yet are still English below, to follow in the next batch.
func msg() locales.Messages { return locales.In(interfaceLanguage) }

// The toolbar, and the menu it holds.
func textFind() string    { return msg().ToolbarFind() }
func textView() string    { return msg().ViewMenu() }
func textRepo() string    { return msg().SwitchRepo() }
func textSplit() string   { return msg().LayoutSplit() }
func textUnified() string { return msg().LayoutUnified() }
func textOneFile() string { return msg().ViewOneFile() }
func textStack() string   { return msg().ViewStack() }
func textWrap() string    { return msg().WordWrap() }
func textSpace() string   { return msg().ShowWhitespace() }
func textTheme() string   { return msg().ThemeMenu() }
func textSystem() string  { return msg().ThemeSystem() }
func textLight() string   { return msg().ThemeLight() }
func textDark() string    { return msg().ThemeDark() }
func textGroup() string   { return msg().GroupMenu() }
func textNoGroup() string { return msg().GroupNone() }
func textByKind() string  { return msg().GroupByKind() }
func textByAI() string    { return msg().GroupByAI() }

// The agent's review, in the state it shows before it ran: there is no
// analysis yet.
func textAIReview() string     { return msg().AiReview() }
func textNotRun() string       { return msg().NotAnalyzed() }
func textOffered() string      { return msg().AiReviewOffered() }
func textReviewWithAI() string { return msg().ReviewWithAI() }

// The bar of an undo.
func textUndo() string { return msg().Undo() }

// Nothing to show, said the same way everywhere: the four the language pass
// has reached.
func textNoRepo() string       { return msg().NoRepository() }
func textNoRepoDetail() string { return msg().NoRepositoryDetail() }
func textNoMatchFiles() string { return msg().NoMatchingFiles() }
func textNoMatchDiffs() string { return msg().NoMatchingDiffs() }
func textAllViewed() string    { return msg().EveryFileViewed() }
func textResetReview() string  { return msg().ResetReview() }

// The review's words the language pass has not reached yet: English still, and
// the batch after this one.
const (
	textReviewFailed = "Failed"
	textTryAgain     = "Try Again"
	textReviewAgain  = "Review Again"
	textStop         = "Stop"
	textStarting     = "Starting the agent"
	textThinking     = "Thinking about the change"
	textReading      = "Reading the files"
	textOrganizing   = "Organizing the review"
	textSettingsSoon = "Not built yet"

	textNoChanges    = "No local changes"
	textNoCommits    = "No changes in commit"
	textNoBranch     = "No changes"
	textNoPullFiles  = "No changes in pull request"
	textNoChangesDir = "No changed files"
	textNoCommitsTab = "No matching commits"
	textNoPulls      = "No pull requests"
	textNoCommands   = "No matching commands"
)
