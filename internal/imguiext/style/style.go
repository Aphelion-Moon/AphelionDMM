package style

import (
	"github.com/SpaiR/imgui-go"
)

type ButtonGreen struct {
}

/* APHELION EDIT REMOVAL START - MERIDIAN THEME
func (ButtonGreen) NormalColor() imgui.Vec4 {
	return ColorGreen1
}

func (ButtonGreen) ActiveColor() imgui.Vec4 {
	return ColorGreen1Darker
}

func (ButtonGreen) HoverColor() imgui.Vec4 {
	return ColorGreen1Lighter
}
APHELION EDIT REMOVAL END */

// APHELION EDIT ADDITION START - MERIDIAN THEME
func (ButtonGreen) NormalColor() imgui.Vec4 { return buttonGreen().Normal }
func (ButtonGreen) ActiveColor() imgui.Vec4 { return buttonGreen().Active }
func (ButtonGreen) HoverColor() imgui.Vec4  { return buttonGreen().Hover }

// APHELION EDIT ADDITION END

type ButtonDefault struct {
}

func (ButtonDefault) NormalColor() imgui.Vec4 {
	return imgui.CurrentStyle().Color(imgui.StyleColorButton)
}

func (ButtonDefault) ActiveColor() imgui.Vec4 {
	return imgui.CurrentStyle().Color(imgui.StyleColorButtonActive)
}

func (ButtonDefault) HoverColor() imgui.Vec4 {
	return imgui.CurrentStyle().Color(imgui.StyleColorButtonHovered)
}

type ButtonGold struct {
}

/* APHELION EDIT REMOVAL START - MERIDIAN THEME
func (ButtonGold) NormalColor() imgui.Vec4 {
	return ColorGold
}

func (ButtonGold) ActiveColor() imgui.Vec4 {
	return ColorGoldDarker
}

func (ButtonGold) HoverColor() imgui.Vec4 {
	return ColorGoldLighter
}
APHELION EDIT REMOVAL END */

// APHELION EDIT ADDITION START - MERIDIAN THEME
func (ButtonGold) NormalColor() imgui.Vec4 { return buttonGold().Normal }
func (ButtonGold) ActiveColor() imgui.Vec4 { return buttonGold().Active }
func (ButtonGold) HoverColor() imgui.Vec4  { return buttonGold().Hover }

// APHELION EDIT ADDITION END

type ButtonRed struct {
}

/* APHELION EDIT REMOVAL START - MERIDIAN THEME
func (ButtonRed) NormalColor() imgui.Vec4 {
	return ColorRed
}

func (ButtonRed) ActiveColor() imgui.Vec4 {
	return ColorRedDarker
}

func (ButtonRed) HoverColor() imgui.Vec4 {
	return ColorRedLighter
}
APHELION EDIT REMOVAL END */

// APHELION EDIT ADDITION START - MERIDIAN THEME
func (ButtonRed) NormalColor() imgui.Vec4 { return buttonRed().Normal }
func (ButtonRed) ActiveColor() imgui.Vec4 { return buttonRed().Active }
func (ButtonRed) HoverColor() imgui.Vec4  { return buttonRed().Hover }

// APHELION EDIT ADDITION END

type ButtonTransparent struct {
}

func (ButtonTransparent) NormalColor() imgui.Vec4 {
	return ColorZero
}

func (ButtonTransparent) ActiveColor() imgui.Vec4 {
	return ColorZero
}

func (ButtonTransparent) HoverColor() imgui.Vec4 {
	return ColorZero
}

type ButtonFrame struct {
}

func (ButtonFrame) NormalColor() imgui.Vec4 {
	return imgui.CurrentStyle().Color(imgui.StyleColorFrameBg)
}

func (ButtonFrame) ActiveColor() imgui.Vec4 {
	return imgui.CurrentStyle().Color(imgui.StyleColorFrameBgActive)
}

func (ButtonFrame) HoverColor() imgui.Vec4 {
	return imgui.CurrentStyle().Color(imgui.StyleColorFrameBgHovered)
}

type ButtonFireCoral struct {
}

func (ButtonFireCoral) NormalColor() imgui.Vec4 {
	return ColorFireCoral
}

func (ButtonFireCoral) ActiveColor() imgui.Vec4 {
	return ColorFireCoralDarker
}

func (ButtonFireCoral) HoverColor() imgui.Vec4 {
	return ColorFireCoralLighter
}
