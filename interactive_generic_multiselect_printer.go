package pterm

import (
	"fmt"
	"sort"

	"strings"

	"atomicgo.dev/cursor"
	"atomicgo.dev/keyboard"
	"atomicgo.dev/keyboard/keys"
	"github.com/lithammer/fuzzysearch/fuzzy"

	"github.com/pterm/pterm/internal"
)

// GenericInteractiveMultiselectPrinter is a printer for interactive multiselect menus.
type GenericInteractiveMultiselectPrinter[T fmt.Stringer] struct {
	DefaultText         string
	TextStyle           *Style
	Options             []T
	OptionStyle         *Style
	MaxHeight           int
	Selector            string
	SelectorStyle       *Style
	Filter              bool
	Checkmark           *Checkmark
	OnInterruptFunc     func()
	ShowSelectedOptions bool
	SelectedOptionStyle *Style

	optionsStr            []string
	selectedOption        int
	selectedOptions       []int
	text                  string
	fuzzySearchString     string
	fuzzySearchMatches    []string
	displayedOptions      []string
	displayedOptionsStart int
	displayedOptionsEnd   int

	// KeySelect is the select key. It cannot be keys.Space when Filter is enabled.
	KeySelect       keys.KeyCode
	EnableSelectAll bool

	// KeyConfirm is the confirm key. It cannot be keys.Space when Filter is enabled.
	KeyConfirm     keys.KeyCode
	EnableClearAll bool
}

func NewGenericInteractiveMultiselect[T fmt.Stringer](options []T) GenericInteractiveMultiselectPrinter[T] {
	return GenericInteractiveMultiselectPrinter[T]{
		TextStyle:           &ThemeDefault.PrimaryStyle,
		DefaultText:         "Please select your options",
		Options:             options,
		OptionStyle:         &ThemeDefault.DefaultText,
		MaxHeight:           5,
		Selector:            ">",
		SelectorStyle:       &ThemeDefault.SecondaryStyle,
		Filter:              true,
		KeySelect:           keys.Enter,
		KeyConfirm:          keys.Tab,
		Checkmark:           &ThemeDefault.Checkmark,
		ShowSelectedOptions: false,
		SelectedOptionStyle: &ThemeDefault.SecondaryStyle,
		EnableSelectAll:     true,
		EnableClearAll:      true,
	}
}

// WithOptions sets the options.
func (p GenericInteractiveMultiselectPrinter[T]) WithOptions(options []T) *GenericInteractiveMultiselectPrinter[T] {
	p.Options = options

	p.optionsStr = make([]string, len(options))
	for i, option := range options {
		p.optionsStr[i] = option.String()
	}

	return &p
}

// WithDefaultText sets the default text.
func (p GenericInteractiveMultiselectPrinter[T]) WithDefaultText(text string) *GenericInteractiveMultiselectPrinter[T] {
	p.DefaultText = text
	return &p
}

// WithMaxHeight sets the maximum height of the select menu.
func (p GenericInteractiveMultiselectPrinter[T]) WithMaxHeight(maxHeight int) *GenericInteractiveMultiselectPrinter[T] {
	p.MaxHeight = maxHeight
	return &p
}

// WithFilter sets the Filter option
func (p GenericInteractiveMultiselectPrinter[T]) WithFilter(b ...bool) *GenericInteractiveMultiselectPrinter[T] {
	p.Filter = internal.WithBoolean(b)
	return &p
}

// WithKeySelect sets the confirm key
// It cannot be keys.Space when Filter is enabled.
func (p GenericInteractiveMultiselectPrinter[T]) WithKeySelect(keySelect keys.KeyCode) *GenericInteractiveMultiselectPrinter[T] {
	p.KeySelect = keySelect
	return &p
}

// WithKeyConfirm sets the confirm key
// It cannot be keys.Space when Filter is enabled.
func (p GenericInteractiveMultiselectPrinter[T]) WithKeyConfirm(keyConfirm keys.KeyCode) *GenericInteractiveMultiselectPrinter[T] {
	p.KeyConfirm = keyConfirm
	return &p
}

// WithCheckmark sets the checkmark
func (p GenericInteractiveMultiselectPrinter[T]) WithCheckmark(checkmark *Checkmark) *GenericInteractiveMultiselectPrinter[T] {
	p.Checkmark = checkmark
	return &p
}

// OnInterrupt sets the function to execute on exit of the input reader
func (p GenericInteractiveMultiselectPrinter[T]) WithOnInterruptFunc(exitFunc func()) *GenericInteractiveMultiselectPrinter[T] {
	p.OnInterruptFunc = exitFunc
	return &p
}

// WithShowSelectedOption shows the selected options at the bottom if the menu
func (p GenericInteractiveMultiselectPrinter[T]) WithShowSelectedOptions(b bool) *GenericInteractiveMultiselectPrinter[T] {
	p.ShowSelectedOptions = b
	return &p
}

// WithSelectedOptionStyle sets the style of the selected options shown at the bottom of the menu
// only used if ShowSelectedOptions is true
func (p GenericInteractiveMultiselectPrinter[T]) WithSelectedOptionStyle(style *Style) *GenericInteractiveMultiselectPrinter[T] {
	p.SelectedOptionStyle = style
	return &p
}

// WithSelectAllEnabled enables the select all feature
// i.e. all options can be selected with the right arrow key
func (p GenericInteractiveMultiselectPrinter[T]) WithSelectAllEnabled(b bool) *GenericInteractiveMultiselectPrinter[T] {
	p.EnableSelectAll = b
	return &p
}

func (p GenericInteractiveMultiselectPrinter[T]) WithClearAllEnabled(b bool) *GenericInteractiveMultiselectPrinter[T] {
	p.EnableClearAll = b
	return &p
}

// Show shows the interactive multiselect menu and returns the selected entry.
func (p *GenericInteractiveMultiselectPrinter[T]) Show(text ...string) ([]T, error) {
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
		p.MaxHeight = DefaultInteractiveMultiselect.MaxHeight
	}

	maxHeight := p.MaxHeight
	if maxHeight > len(p.fuzzySearchMatches) {
		maxHeight = len(p.fuzzySearchMatches)
	}

	if len(p.Options) == 0 {
		return nil, fmt.Errorf("no options provided")
	}

	p.displayedOptions = append([]string{}, p.fuzzySearchMatches[:maxHeight]...)
	p.displayedOptionsStart = 0
	p.displayedOptionsEnd = maxHeight

	area, err := DefaultArea.Start(p.renderSelectMenu())
	defer area.Stop()
	if err != nil {
		return nil, fmt.Errorf("could not start area: %w", err)
	}

	if p.Filter && (p.KeyConfirm == keys.Space || p.KeySelect == keys.Space) {
		return nil, fmt.Errorf("if filter/search is active, keys.Space can not be used for KeySelect or KeyConfirm")
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
		case p.KeyConfirm:
			if len(p.fuzzySearchMatches) == 0 {
				return false, nil
			}
			area.Update(p.renderFinishedMenu())
			return true, nil
		case p.KeySelect:
			if len(p.fuzzySearchMatches) > 0 {
				// Select option if not already selected
				p.selectOption(p.fuzzySearchMatches[p.selectedOption])
			}
			area.Update(p.renderSelectMenu())
		case keys.RuneKey:
			if p.Filter {
				// Fuzzy search for options
				// append to fuzzy search string
				p.fuzzySearchString += keyInfo.String()
				p.selectedOption = 0
				p.displayedOptionsStart = 0
				p.displayedOptionsEnd = maxHeight
				p.displayedOptions = append([]string{}, p.fuzzySearchMatches[:maxHeight]...)
			}
			area.Update(p.renderSelectMenu())
		case keys.Space:
			if p.Filter {
				p.fuzzySearchString += " "
				p.selectedOption = 0
				area.Update(p.renderSelectMenu())
			}
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
		case keys.Left:
			if p.EnableClearAll {
				// Unselect all options
				p.selectedOptions = []int{}
				area.Update(p.renderSelectMenu())
			}
		case keys.Right:
			if p.EnableSelectAll {
				// Select all options
				p.selectedOptions = []int{}
				for i := 0; i < len(p.Options); i++ {
					p.selectedOptions = append(p.selectedOptions, i)
				}
				area.Update(p.renderSelectMenu())
			}
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
		}

		return false, nil
	})
	if err != nil {
		Error.Println(err)
		return nil, fmt.Errorf("failed to start keyboard listener: %w", err)
	}

	result := p.getSelectedOptions()
	return result, nil
}

func (p GenericInteractiveMultiselectPrinter[T]) findOptionByText(text string) int {
	for i, option := range p.optionsStr {
		if option == text {
			return i
		}
	}
	return -1
}

func (p *GenericInteractiveMultiselectPrinter[T]) isSelected(optionText string) bool {
	for _, selectedOption := range p.selectedOptions {
		if p.optionsStr[selectedOption] == optionText {
			return true
		}
	}

	return false
}

func (p *GenericInteractiveMultiselectPrinter[T]) selectOption(optionText string) {
	if p.isSelected(optionText) {
		// Remove from selected options
		for i, selectedOption := range p.selectedOptions {
			if p.optionsStr[selectedOption] == optionText {
				p.selectedOptions = append(p.selectedOptions[:i], p.selectedOptions[i+1:]...)
				break
			}
		}
	} else {
		// Add to selected options
		p.selectedOptions = append(p.selectedOptions, p.findOptionByText(optionText))
	}
}

func (p *GenericInteractiveMultiselectPrinter[T]) renderSelectMenu() string {
	var content string
	content += Sprintf("%s: %s\n", p.text, p.fuzzySearchString)

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
		var checkmark string
		if p.isSelected(option) {
			checkmark = fmt.Sprintf("[%s]", p.Checkmark.Checked)
		} else {
			checkmark = fmt.Sprintf("[%s]", p.Checkmark.Unchecked)
		}
		if i == p.selectedOption {
			content += Sprintf("%s %s %s\n", p.renderSelector(), checkmark, option)
		} else {
			content += Sprintf("  %s %s\n", checkmark, option)
		}
	}

	// TODO? Use a string builder
	var help string
	if p.EnableSelectAll {
		help = fmt.Sprintf("%s: %s | %s: %s | left: %s | right: %s",
			p.KeySelect, Bold.Sprint("select"),
			p.KeyConfirm, Bold.Sprint("confirm"),
			Bold.Sprint("clear selection"),
			Bold.Sprint("select all"),
		)
	} else {
		help = fmt.Sprintf("%s: %s | %s: %s | left: %s",
			p.KeySelect, Bold.Sprint("select"),
			p.KeyConfirm, Bold.Sprint("confirm"),
			Bold.Sprint("clear selection"),
		)
	}

	if p.Filter {
		help += fmt.Sprintf(" | type to %s", Bold.Sprint("filter"))
	}
	content += ThemeDefault.SecondaryStyle.Sprintfln(help)

	if len(p.selectedOptions) > 0 {
		content += p.SelectedOptionStyle.Sprint("you have selected: ")

		selected := p.getSelectedOptionsStr()

		content += p.SelectedOptionStyle.Add(*Italic.ToStyle()).
			Sprintln(strings.Join(selected, ", "))
	}

	return content
}

func (p GenericInteractiveMultiselectPrinter[T]) renderFinishedMenu() string {
	var content string
	content += Sprintf("%s: %s\n", p.text, p.fuzzySearchString)
	for _, option := range p.selectedOptions {
		content += Sprintf("  %s %s\n", p.renderSelector(), p.Options[option])
	}

	return content
}

func (p GenericInteractiveMultiselectPrinter[T]) renderSelector() string {
	return p.SelectorStyle.Sprint(p.Selector)
}

func (p GenericInteractiveMultiselectPrinter[T]) getSelectedOptions() []T {
	selected := make([]T, len(p.selectedOptions))
	for i, selectedOption := range p.selectedOptions {
		selected[i] = p.Options[selectedOption]
	}
	return selected
}

func (p GenericInteractiveMultiselectPrinter[T]) getSelectedOptionsStr() []string {
	selected := make([]string, len(p.selectedOptions))
	for i, selectedOption := range p.selectedOptions {
		selected[i] = p.optionsStr[selectedOption]
	}
	return selected
}
