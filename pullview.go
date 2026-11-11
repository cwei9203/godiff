package main

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/egoist/godiff/internal/github"
	"github.com/egoist/mygo/ui"
)

// pullsView lists the open pull requests of the repository's remote on
// GitHub, in the sidebar.
func (w *window) pullsView(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	if !w.pullsLoaded && !w.pullsLoading {
		w.loadPulls()
	}
	q := strings.ToLower(strings.TrimSpace(w.pullsFilter))
	var list []*github.Summary
	for i := range w.pulls {
		p := &w.pulls[i]
		if q == "" || strings.Contains(strings.ToLower(p.Title), q) || strings.Contains(strings.ToLower(p.Author.Login), q) ||
			strings.TrimPrefix(q, "#") == itoa(p.Number) || strings.Contains(strings.ToLower(p.HeadRef), q) {
			list = append(list, p)
		}
	}
	ui.Row(c).Padding(0, 10, 4, 14).Gap(6).Children(func() {
		label := msg().PullOpenOn(w.gh.repo.String())
		if w.pullsLoaded && w.pullsErr == nil {
			label = msg().PullOpenCount(len(w.pulls), w.gh.repo.String())
		}
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(t.TextMuted).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		if w.pullsLoading {
			ui.Spinner(c).Size(12, 12)
		}
		if iconButton(c, iconRefresh, msg().RefreshPullRequests()).Size(22, 22).Clicked() {
			w.loadPulls()
		}
	})
	if len(list) == 0 {
		box := emptyBox{shape: emptySmall, icon: iconPull, title: msg().NoOpenPulls(), actions: func() {
			if ui.Button(c, msg().OpenPullMenu()).Clicked() {
				w.openDialog(dialogPull)
			}
		}}
		switch {
		case w.pullsErr != nil:
			box.title, box.detail = msg().UnableListPulls(), errorText(w.pullsErr)
			box.actions = func() {
				if ui.Button(c, msg().TryAgain()).Clicked() {
					w.loadPulls()
				}
			}
		case !w.pullsLoaded:
			box.title, box.detail = msg().LoadingPulls(), ""
		case q != "":
			box.title, box.detail = textNoPulls(), q
		}
		emptyState(c, pal, box)
		return
	}
	current := -1
	for i, p := range list {
		if w.source == pullSource(w.gh.repo, p.Number) {
			current = i
		}
	}
	open := func(i int) {
		if i < 0 || i >= len(list) {
			return
		}
		w.commitOpen = false
		w.pullsList.ScrollIntoView(i)
		w.setSource(pullSource(w.gh.repo, list[i].Number))
	}
	w.pullsList.Key = func(i int) any { return list[i].Number }
	w.pullsList.Label = func(i int) string { return list[i].Title }
	rows := ui.List(c, &w.pullsList, len(list), func(i int) {
		focused := w.pullsList.FocusWithin(c)
		p := list[i]
		row := ui.Row(c).Gap(8).Padding(6, 8).Radius(6).AlignItems(ui.Start).Role(ui.RoleButton).Label(p.Title)
		muted, ref := t.TextMuted, pal.ref
		selected := i == current && focused
		switch {
		case selected:
			// The pull request the surface shows: a low ground, the body
			// color kept, as the rows the keys are on.
			row.Background(pal.selection)
		case i == current:
			row.Background(pal.surfaceHover)
		case row.Hovered():
			row.Background(pal.hover)
		}
		if (row.Pressed() || row.Clicked()) && i != current {
			open(i)
		}
		row.Tooltip(fmt.Sprintf("#%d %s\n%s wants to merge %s", p.Number, p.Title, p.Author.Login, p.HeadRef))
		row.Children(func() {
			icon, color := iconPull, pal.addBar
			if p.Draft {
				icon, color = iconPullDraft, t.TextMuted
			}
			if selected {
				color = t.AccentText
			}
			ui.Icon(c, icon).FontSize(14).TextColor(color).Margin(1, 0, 0).Shrink(0)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, p.Title).FontSize(12).FontWeight(500).MaxLines(2)
				ui.Row(c).Gap(5).Children(func() {
					ui.Textf(c, "#%d", p.Number).Font(w.codeFont()).FontSize(10).TextColor(ref).Shrink(0)
					ui.Text(c, p.Author.Login).FontSize(10).TextColor(muted).SingleLine().Shrink(1).MinWidth(0)
					ui.Text(c, relativeTime(w.now, p.UpdatedAt)).FontSize(10).TextColor(muted).Shrink(0)
					ui.Spacer(c)
					switch p.ReviewDecision {
					case "approved":
						ui.Icon(c, iconCheck).FontSize(12).TextColor(pickColor(selected, t.AccentText, pal.addBar)).Tooltip(msg().ApprovedTip()).Shrink(0)
					case "changes_requested":
						ui.Icon(c, iconXCircle).FontSize(12).TextColor(pickColor(selected, t.AccentText, t.Danger)).Tooltip(msg().ChangesRequestedTip()).Shrink(0)
					}
					if p.Checks != "" {
						color := map[string]ui.Color{"success": pal.addBar, "failure": t.Danger, "pending": pal.warning}[p.Checks]
						ui.Box(c).Size(7, 7).Radius(4).Background(color).Tooltip(msg().ChecksTip(p.Checks)).Shrink(0)
					}
				})
			})
		})
	}).Grow(1).Padding(2, 8).Gap(1).Focusable().Label(msg().PullRequestsLabel())
	focusRing(rows, pal, 6)
	if w.pullsList.Shortcut(c, 0, ui.KeyDown) {
		open(current + 1)
	}
	if w.pullsList.Shortcut(c, 0, ui.KeyUp) {
		open(max(current-1, 0))
	}
}

// ago is " · 3d ago", "" for no time.
func ago(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return " · " + relativeTime(now, t)
}

// stateLabel names a pull request's state in the interface's language.
func stateLabel(status string) string {
	switch status {
	case "Draft":
		return msg().PullDraft()
	case "Merged":
		return msg().PullMerged()
	case "Closed":
		return msg().PullClosed()
	}
	return msg().PullOpen()
}

func pickColor(on bool, a, b ui.Color) ui.Color {
	if on {
		return a
	}
	return b
}

// stateBadge is the pull request's state as GitHub shows it: Open,
// Draft, Merged or Closed.
func stateBadge(c *ui.Context, pal *palette, m *github.PullRequest) ui.Element {
	t := c.Theme()
	status := m.Status()
	icon, color := iconPull, pal.addBar
	switch status {
	case "Draft":
		icon, color = iconPullDraft, t.TextMuted
	case "Merged":
		icon, color = iconMerge, ui.Hex("#8250df")
	case "Closed":
		icon, color = iconPullClosed, t.Danger
	}
	return ui.Row(c).Gap(5).Padding(4, 10, 4, 8).Radius(14).Background(color).TextColor(ui.RGB(255, 255, 255)).Shrink(0).Children(func() {
		ui.Icon(c, icon).FontSize(13)
		ui.Text(c, stateLabel(status)).FontSize(12).FontWeight(700)
	})
}

var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// plainBody is the Markdown of a description as text: without the HTML
// comments of templates, nor the blank lines they leave.
func plainBody(s string) string {
	s = htmlComment.ReplaceAllString(strings.ReplaceAll(s, "\r\n", "\n"), "")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

// pullHeader shows the pull request reviewed: its state, title, branches,
// description and reviews.
func (w *window) pullHeader(c *ui.Context, pal *palette) ui.Element {
	t := c.Theme()
	p := w.pr
	m := p.meta
	return ui.Column(c).Padding(padCard, 16).Gap(10).Radius(radiusCard).Background(pal.surface).Border(1, pal.border).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
			stateBadge(c, pal, m)
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.RichText(c,
					ui.Span{Text: m.Title, Weight: 700},
					ui.Span{Text: fmt.Sprintf("  #%d", m.Number), Color: t.TextMuted},
				).FontSize(15).Selectable()
				ui.Row(c).Gap(6).Children(func() {
					avatar(c, m.User, 18)
					verb := msg().PullWantsToMerge(msg().CommitsCount(m.Commits))
					if m.Status() == "Merged" {
						verb = msg().PullMergedWants(msg().CommitsCount(m.Commits))
					}
					ui.RichText(c,
						ui.Span{Text: m.User.Login, Weight: 600},
						ui.Span{Text: verb, Color: t.TextMuted},
						ui.Span{Text: m.Base.Ref, Font: w.codeFont(), Color: pal.ref},
						ui.Span{Text: msg().PullFrom(), Color: t.TextMuted},
						ui.Span{Text: m.Head.Label, Font: w.codeFont(), Color: pal.ref},
						ui.Span{Text: ago(w.now, m.CreatedAt), Color: t.TextMuted},
					).FontSize(12).Shrink(1).MinWidth(0).Tooltip(msg().PullOpenedAt(m.CreatedAt.Local().Format("Mon Jan 2 15:04 2006")))
				})
			})
			if iconButton(c, iconCopy, msg().CopyLink()).Clicked() {
				c.WriteClipboard(m.HTMLURL)
				c.Toast(msg().CopiedLink(m.Number))
			}
			if iconButton(c, iconOpen, msg().OpenOnGitHub()).Clicked() {
				c.OpenURL(m.HTMLURL)
			}
		})
		ui.Row(c).Gap(10).Children(func() {
			ui.RichText(c,
				ui.Span{Text: "+" + thousands(m.Additions), Color: pal.addText},
				ui.Span{Text: " −" + thousands(m.Deletions), Color: pal.delText},
				ui.Span{Text: " · " + msg().FilesCount(m.ChangedFiles), Color: t.TextMuted},
			).Font(w.codeFont()).FontSize(12).FontWeight(600)
			if r := p.reviews; r != nil && len(r.Threads) > 0 {
				open := 0
				for _, th := range r.Threads {
					if !th.Resolved && !th.Outdated {
						open++
					}
				}
				ui.Text(c, msg().PullThreadsUnresolved(msg().ThreadsCount(len(r.Threads)), open)).FontSize(12).TextColor(t.TextMuted)
			}
			if p.reviewsLoading {
				ui.Spinner(c).Size(12, 12)
			}
		})
		if body := plainBody(m.Body); body != "" {
			ui.Column(c).Gap(4).Children(func() {
				text := ui.Text(c, body).FontSize(13).Selectable().TextColor(t.Text)
				long := strings.Count(body, "\n") > 6 || len(body) > 600
				if long && !p.descOpen {
					text.MaxLines(6)
				}
				if long {
					label := msg().ShowMore()
					if p.descOpen {
						label = msg().ShowLess()
					}
					b := ui.ButtonBase(c).AlignSelf(ui.Start).Children(func() {
						ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.Accent)
					})
					if b.Clicked() {
						p.descOpen = !p.descOpen
					}
				}
			})
		}
		if r := p.reviews; r != nil && len(r.Summaries) > 0 {
			ui.Column(c).Gap(8).Padding(10, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(pal.border).Children(func() {
				for _, s := range r.Summaries {
					w.reviewSummary(c, pal, s)
				}
			})
		}
		hint := func(icon *ui.SVG, text string, color ui.Color, dismiss func()) {
			ui.Row(c).Gap(8).Padding(6, 6, 6, 10).Radius(8).Background(color.Alpha(0.08)).Children(func() {
				ui.Icon(c, icon).FontSize(14).TextColor(color)
				ui.Text(c, text).FontSize(12).TextColor(color.Mix(t.Text, 0.4)).Grow(1).Shrink(1).MinWidth(0).Selectable()
				if dismiss != nil && iconButton(c, iconClose, msg().Dismiss()).Size(22, 22).Clicked() {
					dismiss()
				}
			})
		}
		if p.writeErr != "" {
			hint(iconAlert, p.writeErr, t.Danger, func() { p.writeErr = "" })
		}
		if p.reviewsErr != nil {
			hint(iconAlert, msg().CouldNotReadReviews(errorText(p.reviewsErr)), t.Danger, nil)
		}
		if p.readOnly != "" {
			hint(iconAlert, p.readOnly, t.TextMuted, nil)
		}
	})
}

// reviewSummary shows a submitted review: who, their verdict, and what
// they wrote.
func (w *window) reviewSummary(c *ui.Context, pal *palette, s github.ReviewSummary) {
	t := c.Theme()
	verdict, color, icon := msg().PullCommented(), t.TextMuted, iconComment
	switch s.State {
	case "approved":
		verdict, color, icon = msg().PullApproved(), pal.addBar, iconCheckCircle
	case "changes_requested":
		verdict, color, icon = msg().PullRequestedChanges(), t.Danger, iconXCircle
	case "dismissed":
		verdict = msg().PullDismissed()
	}
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			ui.Icon(c, icon).FontSize(14).TextColor(color)
			avatar(c, s.Author, 18)
			ui.RichText(c,
				ui.Span{Text: s.Author.Login, Weight: 600},
				ui.Span{Text: " " + verdict, Color: color},
				ui.Span{Text: ago(w.now, s.SubmittedAt), Color: t.TextMuted},
			).FontSize(12)
		})
		if body := plainBody(s.Body); body != "" {
			ui.Text(c, body).FontSize(13).Selectable().Padding(0, 0, 0, 44).TextColor(t.Text)
		}
	})
}

// openThreads counts a file's threads neither resolved nor outdated.
func (w *window) openThreads(path string) int {
	n := 0
	for _, th := range w.threads(path) {
		if !th.Resolved && !th.Outdated {
			n++
		}
	}
	return n
}

// threadLabel is where a thread is: "Line 12", "Old lines 3-5".
func threadLabel(th *github.Thread) string {
	prefix := msg().LineLabel()
	if th.Side == github.Left {
		prefix = msg().OldLineLabel()
	}
	line := th.Line
	if line == 0 {
		line = th.OriginalLine
	}
	if th.StartLine != 0 && th.StartLine != line {
		return msg().ThreadLineRange(prefix, th.StartLine, line)
	}
	return fmt.Sprintf("%s %d", prefix, line)
}

// threadRow shows a review thread of the pull request under the line it
// is on: its comments, a reply, and resolving it. Resolved and outdated
// threads show folded.
func (w *window) threadRow(c *ui.Context, pal *palette, f *fileState, th *github.Thread) {
	t := c.Theme()
	p := w.pr
	if p == nil {
		ui.Box(c).Height(0)
		return
	}
	folded := (th.Resolved || th.Outdated) && !p.opened[th.RootID]
	w.card(c, pal).Padding(8, 16).Children(func() {
		box := ui.Column(c).Grow(1).MinWidth(0).Radius(12).Border(1, pal.border).Background(pal.surface).Clip()
		if th.Pending {
			box.Border(1, pal.ref.Alpha(0.45))
		}
		box.Children(func() {
			head := ui.Row(c).Height(34).Padding(0, 4, 0, 10).Gap(8)
			if !folded {
				head.BorderWidth(0, 0, 1, 0).BorderColor(pal.border)
			}
			head.Children(func() {
				ui.Icon(c, iconComment).FontSize(13).TextColor(t.TextMuted)
				ui.Text(c, threadLabel(th)).FontSize(11).FontWeight(600).TextColor(t.TextMuted).Shrink(0)
				badge := func(text string, color ui.Color) {
					ui.Text(c, text).FontSize(10).FontWeight(700).Padding(1, 6).Radius(8).Background(color.Alpha(0.14)).TextColor(color).Shrink(0)
				}
				if th.Pending {
					badge(msg().PendingBadge(), pal.ref)
				}
				if th.Resolved {
					badge(msg().ResolvedBadge(), pal.viewed)
				}
				if th.Outdated {
					badge(msg().OutdatedBadge(), t.TextMuted)
				}
				if folded && len(th.Comments) > 0 {
					first := th.Comments[0]
					ui.RichText(c,
						ui.Span{Text: first.Author.Login + " ", Weight: 600},
						ui.Span{Text: headLine(plainBody(first.Body)), Color: t.TextMuted},
					).FontSize(12).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				} else {
					ui.Spacer(c)
				}
				if th.Resolved || th.Outdated {
					label := msg().ShowAction()
					if !folded {
						label = msg().HideAction()
					}
					b := ui.ButtonBase(c).Padding(3, 8).Radius(6).Children(func() {
						ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(t.Accent)
					})
					if b.Clicked() {
						p.opened[th.RootID] = folded
					}
				}
				if p.canWrite() && th.NodeID != "" && !th.Pending {
					label := msg().ResolveAction()
					if th.Resolved {
						label = msg().UnresolveAction()
					}
					b := ui.Button(c, label).Height(24).Disabled(p.busy > 0)
					if b.Clicked() {
						resolved := !th.Resolved
						w.write(p, func(ctx context.Context, cl *github.Client) error {
							return cl.Resolve(ctx, th.NodeID, resolved)
						}, nil)
					}
				}
				if len(th.Comments) > 0 && th.Comments[0].URL != "" {
					if iconButton(c, iconOpen, msg().OpenOnGitHub()).Size(24, 24).Clicked() {
						c.OpenURL(th.Comments[0].URL)
					}
				}
			})
			if folded {
				return
			}
			for _, cm := range th.Comments {
				w.threadComment(c, pal, th, cm)
			}
			if p.canWrite() && !th.Pending {
				w.replyBox(c, pal, th)
			}
		})
	})
}

// threadComment shows a comment of a thread; the viewer's own can be
// edited and deleted.
func (w *window) threadComment(c *ui.Context, pal *palette, th *github.Thread, cm *github.Comment) {
	t := c.Theme()
	p := w.pr
	own := p.viewer != "" && cm.Author.Login == p.viewer && p.canWrite()
	ui.Row(c.Key(cm.ID)).Gap(10).Padding(10, 12).AlignItems(ui.Start).Children(func() {
		avatar(c, cm.Author, 24)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				ui.Text(c, cm.Author.Login).FontSize(12).Bold()
				ui.Text(c, relativeTime(w.now, cm.CreatedAt)).FontSize(11).TextColor(t.TextMuted).
					Tooltip(cm.CreatedAt.Local().Format("Mon Jan 2 15:04 2006"))
				ui.Spacer(c)
				if own && p.editing != cm.ID {
					if iconButton(c, iconPencil, msg().EditComment()).Size(22, 22).Clicked() {
						p.editing, p.editText = cm.ID, cm.Body
					}
					if iconButton(c, iconTrash, msg().DeleteComment()).Size(22, 22).Disabled(p.busy > 0).Clicked() {
						id := cm.ID
						w.write(p, func(ctx context.Context, cl *github.Client) error {
							return cl.DeleteComment(ctx, p.repo, id)
						}, nil)
					}
				}
			})
			if p.editing == cm.ID {
				area := ui.TextAreaBase(c, &p.editText).MinHeight(60).Padding(8, 10).FontSize(13).Radius(8).
					Background(pal.codeBg).Border(1, t.Accent.Alpha(0.5)).Label(msg().EditComment()).AutoFocus()
				if area.Focused() {
					w.typing = true
				}
				save := func() {
					id, body := cm.ID, strings.TrimSpace(p.editText)
					if body == "" {
						return
					}
					w.write(p, func(ctx context.Context, cl *github.Client) error {
						return cl.EditComment(ctx, p.repo, id, body)
					}, func(err error) {
						if err == nil {
							p.editing = 0
						}
					})
				}
				if area.Shortcut(ui.Cmd, ui.KeyEnter) {
					save()
				}
				if area.Shortcut(0, ui.KeyEscape) {
					p.editing = 0
				}
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, msg().Cancel()).Clicked() {
						p.editing = 0
					}
					if ui.PrimaryButton(c, msg().SaveAction()).Disabled(p.busy > 0 || strings.TrimSpace(p.editText) == "").Clicked() {
						save()
					}
				})
				return
			}
			ui.Text(c, plainBody(cm.Body)).FontSize(13).Selectable()
		})
	})
}

// replyBox is the field answering a thread.
func (w *window) replyBox(c *ui.Context, pal *palette, th *github.Thread) {
	t := c.Theme()
	p := w.pr
	text := p.replies[th.RootID]
	if text == nil {
		text = new(string)
		p.replies[th.RootID] = text
	}
	ui.Column(c).Gap(8).Padding(8, 12, 10).BorderWidth(1, 0, 0, 0).BorderColor(pal.border).Children(func() {
		area := ui.TextAreaBase(c, text).Placeholder(msg().ReplyPlaceholder()).MinHeight(34).Padding(7, 10).FontSize(13).Radius(8).
			Background(pal.codeBg).Border(1, pal.border).Label(msg().ReplyToThread())
		focused := area.Focused()
		if focused {
			w.typing = true
			area.Border(1, t.Accent.Alpha(0.6))
		}
		send := func() {
			body := strings.TrimSpace(*text)
			if body == "" || p.busy > 0 {
				return
			}
			root := th.RootID
			w.write(p, func(ctx context.Context, cl *github.Client) error {
				return cl.Reply(ctx, p.repo, p.number, root, body)
			}, func(err error) {
				if err == nil {
					*text = ""
				}
			})
		}
		if area.Shortcut(ui.Cmd, ui.KeyEnter) {
			send()
		}
		if strings.TrimSpace(*text) != "" {
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Text(c, msg().ReplyHint()).FontSize(11).TextColor(t.TextMuted)
				if ui.PrimaryButton(c, msg().ReplyAction()).Disabled(p.busy > 0).Clicked() {
					send()
				}
			})
		}
	})
}

// prCommentActions are the buttons of a draft comment on a pull request:
// post it alone, or into the viewer's review.
func (w *window) prCommentActions(c *ui.Context, pal *palette, cm *comment) {
	t := c.Theme()
	p := w.pr
	if p == nil {
		return
	}
	ui.Row(c).Gap(8).Padding(8, 10).BorderWidth(1, 0, 0, 0).BorderColor(pal.border).Children(func() {
		switch {
		case cm.postErr != "":
			ui.Text(c, cm.postErr).FontSize(12).TextColor(t.Danger).Grow(1).Shrink(1).MinWidth(0).MaxLines(3).Selectable()
		case !p.canWrite():
			ui.Text(c, msg().KeptHere()).FontSize(12).TextColor(t.TextMuted).Grow(1).Shrink(1).MinWidth(0)
			return
		default:
			ui.Text(c, "⌘↩ adds it to your review").FontSize(11).TextColor(t.TextMuted).Grow(1).Shrink(1).MinWidth(0)
		}
		can := cm.pending() && !cm.posting
		if ui.Button(c, msg().AddSingleComment()).Disabled(!can).Clicked() {
			w.postComment(cm, false)
		}
		label := msg().StartReview()
		if p.reviews != nil && p.reviews.Pending != nil {
			label = msg().AddReviewComment()
		}
		if cm.posting {
			label = msg().Posting()
		}
		if ui.PrimaryButton(c, label).Disabled(!can).Clicked() {
			w.postComment(cm, true)
		}
	})
}

// reviewButton opens the dialog submitting the viewer's review.
func (w *window) reviewButton(c *ui.Context, pal *palette) {
	p := w.pr
	if p == nil || p.meta == nil {
		return
	}
	pending := 0
	if p.reviews != nil {
		pending = p.reviews.PendingComments()
	}
	b := ui.Button(c, "").Height(28).Disabled(!p.canWrite()).Children(func() {
		ui.Icon(c, iconCheckCircle).FontSize(14)
		ui.Text(c, msg().ReviewLabel()).SingleLine()
		if pending > 0 {
			ui.Text(c, itoa(pending)).FontSize(11).FontWeight(700).Padding(0, 6).Radius(8).Background(pal.ref.Alpha(0.2)).TextColor(pal.ref)
		}
	})
	if p.readOnly != "" {
		b.Tooltip(p.readOnly)
	} else {
		b.Tooltip(msg().SubmitYourReview())
	}
	if b.Clicked() {
		p.submitOpen = true
		p.submitErr = ""
		if p.submitEvent == "" {
			p.submitEvent = github.VerdictComment
		}
	}
}

// reviewDialog submits the viewer's review: its comments pending, a
// summary, and a verdict.
func (w *window) reviewDialog(c *ui.Context) {
	p := w.pr
	if p == nil || !p.submitOpen {
		return
	}
	t := c.Theme()
	own := p.meta != nil && p.viewer != "" && p.meta.User.Login == p.viewer
	if own && p.submitEvent != github.VerdictComment {
		p.submitEvent = github.VerdictComment
	}
	var pending *github.PendingReview
	drafts := 0
	if p.reviews != nil {
		pending, drafts = p.reviews.Pending, p.reviews.PendingComments()
	}
	submit := func() {
		if p.submitBusy {
			return
		}
		body, event := strings.TrimSpace(p.submitBody), p.submitEvent
		if event != github.VerdictApprove && body == "" && drafts == 0 {
			p.submitErr = msg().SubmitReviewEmpty()
			return
		}
		p.submitBusy = true
		p.submitErr = ""
		w.write(p, func(ctx context.Context, cl *github.Client) error {
			return cl.SubmitReview(ctx, p.repo, p.number, pending, event, body)
		}, func(err error) {
			p.submitBusy = false
			if err != nil {
				p.submitErr = errorText(err)
				return
			}
			p.submitOpen = false
			p.submitBody = ""
		})
	}
	ui.Modal(c, &p.submitOpen, func() {
		ui.Column(c).Width(500).Gap(14).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, msg().FinishYourReview()).FontSize(16).Bold()
				detail := msg().PullNumberTitle(p.number, p.meta.Title)
				if drafts > 0 {
					detail = msg().PullReviewPendingWith(msg().PendingCommentsCount(drafts), detail)
				}
				ui.Text(c, detail).FontSize(13).TextColor(t.TextMuted).MaxLines(2)
			})
			area := ui.TextArea(c, &p.submitBody).Placeholder(msg().ReviewSummaryPlaceholder()).MinHeight(110).Label(msg().ReviewSummaryLabel()).AutoFocus()
			if area.Shortcut(ui.Cmd, ui.KeyEnter) {
				submit()
			}
			ui.Column(c).Gap(8).Children(func() {
				option := func(event, label, detail string, disabled bool) {
					r := ui.RadioBase(c, &p.submitEvent, event).Gap(10).Padding(6, 8).Radius(8).AlignItems(ui.Start).Label(label).Disabled(disabled)
					if r.Hovered() && !disabled {
						r.Background(paletteFor(t).hover)
					}
					on := p.submitEvent == event
					r.Children(func() {
						dot := ui.Box(c).Size(16, 16).Radius(8).Center().Shrink(0).Margin(1, 0, 0)
						if on {
							dot.Background(t.Accent).Children(func() { ui.Box(c).Size(6, 6).Radius(3).Background(t.AccentText) })
						} else {
							dot.Border(1.5, ui.RGBA(127, 127, 127, 0.45))
						}
						ui.Column(c).Gap(1).Children(func() {
							ui.Text(c, label).FontSize(13).FontWeight(600)
							ui.Text(c, detail).FontSize(12).TextColor(t.TextMuted)
						})
					})
					if disabled {
						r.Opacity(0.5).Tooltip(msg().OwnPullTip())
					}
				}
				option(github.VerdictComment, msg().SubmitGeneral(), msg().SubmitGeneralDetail(), false)
				option(github.VerdictApprove, msg().SubmitApprove(), msg().SubmitApproveDetail(), own)
				option(github.VerdictRequestChanges, msg().SubmitRequestChanges(), msg().SubmitRequestChangesDetail(), own)
			})
			if p.submitErr != "" {
				ui.Text(c, p.submitErr).FontSize(12).TextColor(t.Danger).Selectable()
			}
			ui.Row(c).Gap(8).Children(func() {
				if pending != nil {
					d := ui.Button(c, msg().DiscardPendingReview()).Disabled(p.submitBusy)
					if d.Clicked() {
						p.submitBusy = true
						w.write(p, func(ctx context.Context, cl *github.Client) error {
							return cl.DiscardReview(ctx, p.repo, p.number, pending)
						}, func(err error) {
							p.submitBusy = false
							if err != nil {
								p.submitErr = errorText(err)
								return
							}
							p.submitOpen = false
						})
					}
				}
				ui.Spacer(c)
				if ui.Button(c, msg().Cancel()).Clicked() {
					p.submitOpen = false
				}
				label := msg().SubmitReviewAction()
				if p.submitBusy {
					label = msg().Submitting()
				}
				if ui.PrimaryButton(c, label).Disabled(p.submitBusy).Clicked() {
					submit()
				}
			})
		})
	})
}

// headLine is the first line of a text.
func headLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// avatars keeps the pictures of GitHub's users, fetched once each.
var avatars struct {
	sync.Mutex
	images  map[string]*ui.Bitmap
	loading map[string]bool
}

// fetchAvatars is set by the app: tests fetch no pictures.
var fetchAvatars bool

// avatar shows a user's picture, their initials until it comes.
func avatar(c *ui.Context, u github.User, size float32) ui.Element {
	img := avatarImage(u)
	if img == nil {
		return ui.Avatar(c, u.Login, nil).Size(size, size).Tooltip(u.Login)
	}
	return ui.Box(c).Size(size, size).Radius(size / 2).Clip().Shrink(0).Tooltip(u.Login).Children(func() {
		ui.Image(c, img).Fit(ui.Cover).Size(size, size)
	})
}

func avatarImage(u github.User) *ui.Bitmap {
	if u.Login == "" {
		return nil
	}
	avatars.Lock()
	defer avatars.Unlock()
	if img, ok := avatars.images[u.Login]; ok || !fetchAvatars || avatars.loading[u.Login] {
		return img
	}
	if avatars.loading == nil {
		avatars.loading, avatars.images = map[string]bool{}, map[string]*ui.Bitmap{}
	}
	avatars.loading[u.Login] = true
	url := u.AvatarURL
	if url == "" {
		url = "https://github.com/" + u.Login + ".png"
	}
	if strings.Contains(url, "?") {
		url += "&s=64"
	} else {
		url += "?s=64"
	}
	go func() {
		var img *ui.Bitmap
		client := &http.Client{Timeout: 20 * time.Second}
		if res, err := client.Get(url); err == nil {
			buf := make([]byte, 0, 16<<10)
			b := make([]byte, 32<<10)
			for {
				n, err := res.Body.Read(b)
				buf = append(buf, b[:n]...)
				if err != nil || len(buf) > 2<<20 {
					break
				}
			}
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				img, _ = ui.DecodeBitmap(buf)
			}
		}
		avatars.Lock()
		avatars.images[u.Login] = img
		avatars.Unlock()
		if img != nil {
			windowsMu.Lock()
			for _, w := range windows {
				w.invalidate()
			}
			windowsMu.Unlock()
		}
	}()
	return nil
}
