package main

// The review's words, written once and in one place: English for now, and the
// language pass that follows takes them from here rather than from every
// corner of the surface.
const (
	// The toolbar, and the menu it holds.
	textFind = "Find in diffs"
	textView = "View"
	// textRepo names the repository switcher of the toolbar.
	textRepo          = "Switch repository"
	textLayoutSplit   = "Split"
	textLayoutUnified = "Unified"
	// textOneFile and textStack name the two file views.
	textOneFile = "One file at a time"
	textStack   = "All files stacked"
	textWrap    = "Word wrap"
	textSpace   = "Show whitespace changes"
	textTheme   = "Theme"
	textThemes  = "Match system"
	textLight   = "Light"
	textDark    = "Dark"
	textGroup   = "Group files"
	textNoGroup = "No groups"
	textByKind  = "By kind"
	textByAI    = "By an AI review"

	// The agent's review, in its three states.
	textAIReview     = "AI Review"
	textReviewWithAI = "Review with AI"
	textReviewAgain  = "Review Again"
	textTryAgain     = "Try Again"
	textStop         = "Stop"
	textNotRun       = "Not started"
	textReviewFailed = "Failed"
	textStarting     = "Starting the agent"
	textThinking     = "Thinking about the change"
	textReading      = "Reading the files"
	textOrganizing   = "Organizing the review"
	textOffered      = "A coding agent on this machine can summarize these changes, group the files by intent and note what deserves attention. It reads, never writes, and starts only when you ask."
	textSettingsSoon = "Not built yet"

	// The bar of an undo.
	textUndo = "Undo"

	// Nothing to show, said the same way everywhere.
	textNoRepo       = "No Git repository found"
	textNoRepoDetail = "Godiff was opened outside a Git repository. Run godiff from inside one, or open a folder below."
	textNoChanges    = "No local changes"
	textNoCommits    = "No changes in commit"
	textNoBranch     = "No changes"
	textNoPullFiles  = "No changes in pull request"
	textNoMatchFiles = "No matching files"
	textNoMatchDiffs = "No matches in diffs"
	textNoChangesDir = "No changed files"
	textNoCommitsTab = "No matching commits"
	textNoPulls      = "No pull requests"
	textNoCommands   = "No matching commands"
	textAllViewed    = "Every file is viewed"
)

// The undo bar's lines, one per action it offers back.
const (
	textResetReview = "Reset the review"
)
