package pterm

import (
	"fmt"
	"math"
	"sort"

	"atomicgo.dev/cursor"
	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/pterm/pterm/internal"
)

var (
// // DefaultInteractiveSelect is the default InteractiveSelect printer.
//
//	DefaultInteractiveSelect = InteractiveGenericSelectPrinter{
//		TextStyle:     &ThemeDefault.PrimaryStyle,
//		DefaultText:   "Please select an option",
//		Options:       []string{},
//		OptionStyle:   &ThemeDefault.DefaultText,
//		DefaultOption: "",
//		MaxHeight:     5,
//		Filter:        true,
//		RenderSelectedOptionFunc: func(s string) string {
//			return Sprintf("  %s %s\n", ">", s)
//		},
//	}
)

// InteractiveGenericSelectPrinter is a printer for interactive select menus.
type InteractiveGenericSelectPrinter[T fmt.Stringer] struct {
	TextStyle                *Style
	DefaultText              string
	Options                  []T
	OptionStyle              *Style
	DefaultOption            T
	MaxHeight                int
	OnInterruptFunc          func()
	Filter                   bool
	RenderSelectedOptionFunc func(string) string

	optionsStr            []string
	defaultOptionStr      string
	selectedOption        int
	result                string
	text                  string
	fuzzySearchString     string
	fuzzySearchMatches    []string
	displayedOptions      []string
	displayedOptionsStart int
	displayedOptionsEnd   int
}

func NewGenericInteractiveSelect[T fmt.Stringer]() InteractiveGenericSelectPrinter[T] {
	return InteractiveGenericSelectPrinter[T]{
		TextStyle:   &ThemeDefault.PrimaryStyle,
		DefaultText: "Please select an option",
		Options:     []T{},
		OptionStyle: &ThemeDefault.DefaultText,
		MaxHeight:   5,
		Filter:      true,
		RenderSelectedOptionFunc: func(s string) string {
			return Sprintf("  %s %s\n", ">", s)
		},
	}
}

// WithDefaultText sets the default text.
func (p InteractiveGenericSelectPrinter[T]) WithDefaultText(text string) *InteractiveGenericSelectPrinter[T] {
	p.DefaultText = text
	return &p
}

// WithOptions sets the options.
func (p InteractiveGenericSelectPrinter[T]) WithOptions(options []T) *InteractiveGenericSelectPrinter[T] {
	p.Options = options
	p.optionsStr = make([]string, len(options))
	for i, option := range options {
		p.optionsStr[i] = option.String()
	}

	return &p
}

// WithDefaultOption sets the default options.
func (p InteractiveGenericSelectPrinter[T]) WithDefaultOption(option T) *InteractiveGenericSelectPrinter[T] {
	p.DefaultOption = option
	p.defaultOptionStr = option.String()
	return &p
}

// WithMaxHeight sets the maximum height of the select menu.
func (p InteractiveGenericSelectPrinter[T]) WithMaxHeight(maxHeight int) *InteractiveGenericSelectPrinter[T] {
	p.MaxHeight = maxHeight
	return &p
}

// OnInterrupt sets the function to execute on exit of the input reader
func (p InteractiveGenericSelectPrinter[T]) WithOnInterruptFunc(exitFunc func()) *InteractiveGenericSelectPrinter[T] {
	p.OnInterruptFunc = exitFunc
	return &p
}

// WithFilter sets the Filter option
func (p InteractiveGenericSelectPrinter[T]) WithFilter(b ...bool) *InteractiveGenericSelectPrinter[T] {
	p.Filter = internal.WithBoolean(b)
	return &p
}

func (p InteractiveGenericSelectPrinter[T]) WithRenderSelectedOptionFunc(f func(string) string) *InteractiveGenericSelectPrinter[T] {
	p.RenderSelectedOptionFunc = f
	return &p
}

// Show shows the interactive select menu and returns the selected entry.
func (p *InteractiveGenericSelectPrinter[T]) Show(text ...string) (T, error) {
	// should be the first defer statement to make sure it is executed last
	// and all the needed cleanup can be done before
	cancel, exit := internal.NewCancelationSignal(p.OnInterruptFunc)
	defer exit()

	if len(text) == 0 || Sprint(text[0]) == "" {
		text = []string{p.DefaultText}
	}

	p.text = p.TextStyle.Sprint(text[0])
	p.fuzzySearchMatches = append([]string{}, p.optionsStr...)

	if p.MaxHeight == 0 {
		p.MaxHeight = DefaultInteractiveSelect.MaxHeight
	}

	maxHeight := p.MaxHeight
	if maxHeight > len(p.fuzzySearchMatches) {
		maxHeight = len(p.fuzzySearchMatches)
	}

	if len(p.Options) == 0 {
		var t T
		return t, fmt.Errorf("no options provided")
	}

	p.displayedOptions = append([]string{}, p.fuzzySearchMatches[:maxHeight]...)
	p.displayedOptionsStart = 0
	p.displayedOptionsEnd = maxHeight

	// Get index of default option
	if p.defaultOptionStr != "" { // TODo? use comparable for T
		for i, option := range p.optionsStr {
			if option == p.defaultOptionStr {
				p.selectedOption = i
				if i > 0 && len(p.Options) > maxHeight {
					p.displayedOptionsEnd = int(math.Min(float64(i-1+maxHeight), float64(len(p.Options))))
					p.displayedOptionsStart = p.displayedOptionsEnd - maxHeight
				} else {
					p.displayedOptionsStart = 0
					p.displayedOptionsEnd = maxHeight
				}
				p.displayedOptions = p.optionsStr[p.displayedOptionsStart:p.displayedOptionsEnd]
				break
			}
		}
	}

	area, err := DefaultArea.Start(p.renderSelectMenu())
	defer area.Stop()
	if err != nil {
		var t T
		return t, fmt.Errorf("could not start area: %w", err)
	}

	area.Update(p.renderSelectMenu())

	cursor.Hide()
	defer cursor.Show()

	err = keyboard.Listen(func(keyInfo keys.Key) (stop bool, err error) {
		key := keyInfo.Code

		if p.MaxHeight > len(p.fuzzySearchMatches) {
			maxHeight = len(p.fuzzySearchMatches)
		} else {
			maxHeight = p.MaxHeight
		}

		switch key {
		case keys.RuneKey:
			if p.Filter {
				// Fuzzy search for options
				// append to fuzzy search string
				p.fuzzySearchString += keyInfo.String()
				p.selectedOption = 0
				p.displayedOptionsStart = 0
				p.displayedOptionsEnd = maxHeight
				p.displayedOptions = append([]string{}, p.fuzzySearchMatches[:maxHeight]...)
				area.Update(p.renderSelectMenu())
			}
		case keys.Space:
			p.fuzzySearchString += " "
			p.selectedOption = 0
			area.Update(p.renderSelectMenu())
		case keys.Backspace:
			// Remove last character from fuzzy search string
			if len(p.fuzzySearchString) > 0 {
				// Handle UTF-8 characters
				p.fuzzySearchString = string([]rune(p.fuzzySearchString)[:len([]rune(p.fuzzySearchString))-1])
			}

			if p.fuzzySearchString == "" {
				p.fuzzySearchMatches = append([]string{}, p.optionsStr...)
			}

			p.renderSelectMenu()

			if len(p.fuzzySearchMatches) > p.MaxHeight {
				maxHeight = p.MaxHeight
			} else {
				maxHeight = len(p.fuzzySearchMatches)
			}

			p.selectedOption = 0
			p.displayedOptionsStart = 0
			p.displayedOptionsEnd = maxHeight
			p.displayedOptions = append([]string{}, p.fuzzySearchMatches[p.displayedOptionsStart:p.displayedOptionsEnd]...)

			area.Update(p.renderSelectMenu())
		case keys.Up, keys.CtrlP:
			if len(p.fuzzySearchMatches) == 0 {
				return false, nil
			}
			if p.selectedOption > 0 {
				p.selectedOption--
				if p.selectedOption < p.displayedOptionsStart {
					p.displayedOptionsStart--
					p.displayedOptionsEnd--
					if p.displayedOptionsStart < 0 {
						p.displayedOptionsStart = 0
						p.displayedOptionsEnd = maxHeight
					}
					p.displayedOptions = append([]string{}, p.fuzzySearchMatches[p.displayedOptionsStart:p.displayedOptionsEnd]...)
				}
			} else {
				p.selectedOption = len(p.fuzzySearchMatches) - 1
				p.displayedOptionsStart = len(p.fuzzySearchMatches) - maxHeight
				p.displayedOptionsEnd = len(p.fuzzySearchMatches)
				p.displayedOptions = append([]string{}, p.fuzzySearchMatches[p.displayedOptionsStart:p.displayedOptionsEnd]...)
			}

			area.Update(p.renderSelectMenu())
		case keys.Down, keys.CtrlN:
			if len(p.fuzzySearchMatches) == 0 {
				return false, nil
			}
			p.displayedOptions = p.fuzzySearchMatches[:maxHeight]
			if p.selectedOption < len(p.fuzzySearchMatches)-1 {
				p.selectedOption++
				if p.selectedOption >= p.displayedOptionsEnd {
					p.displayedOptionsStart++
					p.displayedOptionsEnd++
					p.displayedOptions = append([]string{}, p.fuzzySearchMatches[p.displayedOptionsStart:p.displayedOptionsEnd]...)
				}
			} else {
				p.selectedOption = 0
				p.displayedOptionsStart = 0
				p.displayedOptionsEnd = maxHeight
				p.displayedOptions = append([]string{}, p.fuzzySearchMatches[p.displayedOptionsStart:p.displayedOptionsEnd]...)
			}

			area.Update(p.renderSelectMenu())
		case keys.CtrlC:
			cancel()
			return true, nil
		case keys.Enter:
			if len(p.fuzzySearchMatches) == 0 {
				return false, nil
			}
			area.Update(p.renderFinishedMenu())
			return true, nil
		}

		return false, nil
	})
	if err != nil {
		Error.Println(err)
		var t T
		return t, fmt.Errorf("failed to start keyboard listener: %w", err)
	}

	return p.Options[p.selectedOption], nil
}

func (p *InteractiveGenericSelectPrinter[T]) renderSelectMenu() string {
	var content string
	if p.Filter {
		content += Sprintf("%s %s: %s\n", p.text, ThemeDefault.SecondaryStyle.Sprint("[type to search]"), p.fuzzySearchString)
	} else {
		content += Sprintf("%s:\n", p.text)
	}

	// find options that match fuzzy search string
	rankedResults := fuzzy.RankFindFold(p.fuzzySearchString, p.optionsStr)
	// map rankedResults to fuzzySearchMatches
	p.fuzzySearchMatches = []string{}
	if len(rankedResults) != len(p.Options) {
		sort.Sort(rankedResults)
	}
	for _, result := range rankedResults {
		p.fuzzySearchMatches = append(p.fuzzySearchMatches, result.Target)
	}

	if len(p.fuzzySearchMatches) != 0 {
		p.result = p.fuzzySearchMatches[p.selectedOption]
	}

	indexMapper := make([]string, len(p.fuzzySearchMatches))
	for i := 0; i < len(p.fuzzySearchMatches); i++ {
		// if in displayed options range
		if i >= p.displayedOptionsStart && i < p.displayedOptionsEnd {
			indexMapper[i] = p.fuzzySearchMatches[i]
		}
	}

	for i, option := range indexMapper {
		if option == "" {
			continue
		}
		if i == p.selectedOption {
			content += p.RenderSelectedOptionFunc(option)
			// content += Sprintf("%s %s\n", p.renderSelector(), p.OptionStyle.Sprint(option))
		} else {
			content += Sprintf("  %s\n", p.OptionStyle.Sprint(option))
		}
	}

	return content
}

func (p InteractiveGenericSelectPrinter[T]) renderFinishedMenu() string {
	var content string
	content += Sprintf("%s: %s\n", p.text, p.fuzzySearchString)
	content += p.RenderSelectedOptionFunc(p.result)

	return content
}

// func (p InteractiveGenericSelectPrinter[T]) renderSelector() string {

// return p.SelectorStyle.Sprint(p.Selector)
// }
