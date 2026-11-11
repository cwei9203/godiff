package main

import (
	"github.com/egoist/godiff/internal/diff"
	"github.com/egoist/godiff/locales"
)

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

// The agent's review, as its card says each state, and the words of a
// surface with nothing to show.
func textReviewFailed() string { return msg().ReviewFailed() }
func textTryAgain() string     { return msg().TryAgain() }
func textReviewAgain() string  { return msg().ReviewAgain() }
func textStop() string         { return msg().Stop() }
func textStarting() string     { return msg().StartingTheAgent() }
func textThinking() string     { return msg().ThinkingAboutTheChange() }
func textReading() string      { return msg().ReadingTheFiles() }
func textOrganizing() string   { return msg().OrganizingTheReview() }
func textSettingsSoon() string { return msg().NotBuiltYet() }
func textNoChanges() string    { return msg().NoLocalChanges() }
func textNoCommits() string    { return msg().NoChangesInCommit() }
func textNoBranch() string     { return msg().NoChangesInBranch() }
func textNoPullFiles() string  { return msg().NoChangesInPull() }
func textNoChangesDir() string { return msg().NoChangedFiles() }
func textNoCommitsTab() string { return msg().NoMatchingCommits() }
func textNoPulls() string      { return msg().NoPullRequests() }
func textNoCommands() string   { return msg().NoCommands() }

// textStatus says how a file changed, in the interface's language.
func textStatus(status diff.Status) string {
	switch status {
	case diff.Added:
		return msg().StatusAdded()
	case diff.Deleted:
		return msg().StatusDeleted()
	case diff.Renamed:
		return msg().StatusRenamed()
	case diff.Copied:
		return msg().StatusCopied()
	case diff.TypeChg:
		return msg().StatusTypeChanged()
	case diff.Untracked:
		return msg().StatusUntracked()
	case diff.Conflict:
		return msg().StatusConflict()
	}
	return msg().StatusModified()
}
