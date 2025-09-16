package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"os"
	"os/exec"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type Layout interface {
	Layout([]fyne.CanvasObject, fyne.Size)
	MinSize(objects []fyne.CanvasObject) fyne.Size
}
type horizontalCustomLayout struct {
	widths  []float32
	heights []float32
	tabbing []float32
}
type verticalCustomLayout struct {
	widths  []float32
	heights []float32
}

func (d *horizontalCustomLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var width, height float32

	for i, o := range objects {
		childSize := o.MinSize()
		if i < len(d.widths) {
			width += d.widths[i]
		} else {
			width += childSize.Width
		}

		if i < len(d.heights) && d.heights[i] > height {
			height = d.heights[i]
		} else if childSize.Height > height {
			height = childSize.Height
		}
	}
	return fyne.NewSize(width, height)
}

func (d *horizontalCustomLayout) Layout(objects []fyne.CanvasObject, containerSize fyne.Size) {

	minSize := d.MinSize(objects)

	pos := fyne.NewPos(
		(containerSize.Width-minSize.Width)/2,
		(containerSize.Height-minSize.Height)/2,
	)

	for i, o := range objects {
		var size fyne.Size

		if i < len(d.widths) && i < len(d.heights) {
			size = fyne.NewSize(d.widths[i], d.heights[i])
		} else {
			size = o.MinSize()
		}

		o.Resize(size)
		o.Move(pos)

		pos = pos.Add(fyne.NewPos(size.Width+d.tabbing[i], 0))
	}
}

func (d *verticalCustomLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var width, height float32

	for i, o := range objects {
		childSize := o.MinSize()

		if i < len(d.widths) && d.widths[i] > childSize.Width {
			if d.widths[i] > width {
				width = d.widths[i]
			}
		} else if childSize.Width > width {
			width = childSize.Width
		}

		if i < len(d.heights) {
			height += d.heights[i]
		} else {
			height += childSize.Height
		}
	}

	return fyne.NewSize(width, height)
}

func (d *verticalCustomLayout) Layout(objects []fyne.CanvasObject, containerSize fyne.Size) {

	pos := fyne.NewPos((containerSize.Width-d.MinSize(objects).Width)/2, 0)

	for i, o := range objects {
		var size fyne.Size

		if i < len(d.widths) && i < len(d.heights) {
			size = fyne.NewSize(d.widths[i], d.heights[i])
		} else {
			size = o.MinSize()
		}
		o.Resize(size)
		o.Move(pos)

		pos = pos.Add(fyne.NewPos(0, size.Height))
	}
}

type Source struct {
	ProjectName string `json:"projectName"`
	Sources     struct {
		Cli struct {
			Org string `json:"org"`
		} `json:"cli"`
		Patches struct {
			Org string `json:"org"`
		} `json:"patches"`
		Integrations struct {
			Org string `json:"org"`
		} `json:"integrations"`
	} `json:"sources"`
}

type PatchInfo struct {
	Name                 string               `json:"name"`
	Description          string               `json:"description"`
	CompatiblePackages   []CompatiblePackages `json:"compatiblePackages"`
	Use                  bool                 `json:"use"`
	RequiresDependencies bool                 `json:"requiresIntegrations"`
	Options              []Options            `json:"options"`
}
type Values struct {
	Clone    string `json:"Clone"`
	Default  string `json:"Default"`
	Original string `json:"Original"`
}
type Options struct {
	Key         string `json:"key"`
	Default     any    `json:"default"`
	Values      Values `json:"values"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}
type CompatiblePackages struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
}

type PatchOptionsJSON struct {
	PatchName string         `json:"patchName"`
	Options   []OptionsPatch `json:"options"`
}
type OptionsPatch struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

var version string = "2.3"

// Tables
var patchTable *widget.Table = loadPatchNames()
var patchScroller = container.NewVScroll(patchTable)

var patchesNames []string
var currentPatchesSelected []string
var desc []string
var include []bool
var supportedVersions []string
var nameLength int
var descLength int

var appToPatch string
var packageName string
var supportedApp []string
var dict = make(map[string]string)

var cliSource = "patches/revanced-cli-5.0.1-all.jar"

// Structs
var patches []PatchInfo
var patchOptionJson []PatchOptionsJSON

var orgNames []string

// Downloader
var apkDownloadVersion string = ""
var patchChosen string = ""

// Settings
var AppName string = "Youtube"
var customPackageName string = "com.google.android.youtube"
var ApkLabel string

// Console
var logLabel = widget.NewLabelWithData(logData)
var logData = binding.NewString()
var consoleLog = container.NewVScroll(logLabel)

var patching bool = false
var patchOptionsPath = "patches/patches-to-use.txt"

func clearTable() {
	supportedVersions = []string{}
	patchesNames = []string{}
	desc = []string{}
	include = []bool{}
}

func getOrgNames(sources map[string]Source) []string {
	var orgNames []string

	for _, source := range sources {
		orgNames = append(orgNames, source.Sources.Patches.Org)
	}
	return orgNames
}

func writePatchesTXT() error {
	if len(patches) == 0 {
		return fmt.Errorf("no patches to write")
	}

	patchesToSave := ""

	for _, patch := range currentPatchesSelected {
		patchesToSave = patchesToSave + "\"" + patch + "\" "
	}

	if err := os.WriteFile(patchOptionsPath, []byte(patchesToSave), 0666); err != nil {
		fmt.Print(err)
	}
	writePatchesOptionsJson()
	return nil
}

func writePatchesOptionsJson() error {
	//fmt.Printf("writing options json \n")
	arrayPatchNames := []string{"Custom branding", "Custom branding name for YouTube", "Custom branding YouTube name", "patch-options"}
	arrayKeyNames := []string{"appName", "YouTube_AppName", "YouTubeAppName", "AppName"}
	for z := 0; z < len(patchOptionJson); z++ {
		for i := 0; i < len(arrayPatchNames); i++ {
			if patchOptionJson[z].PatchName == arrayPatchNames[i] {

				for x := 0; x < len(arrayKeyNames); x++ {
					if patchOptionJson[z].Options[0].Key == arrayKeyNames[x] {
						patchOptionJson[z].Options[0].Value = AppName
					}
				}
			}
		}
	}

	// arrayPatchNames = []string{"patch-options", "Change package name", "Custom package name", "GmsCore support"}
	// arrayKeyNames = []string{"YouTube_PackageName", "YouTubePackageName", "packageName", "PackageNameYouTube"}
	// for z := 0; z < len(patchOptionJson); z++ {
	// 	for i := 0; i < len(arrayPatchNames); i++ {
	// 		if patchOptionJson[z].PatchName == arrayPatchNames[i] {

	// 			for x := 0; x < len(arrayKeyNames); x++ {
	// 				if patchOptionJson[z].Options[0].Key == arrayKeyNames[x] {
	// 					patchOptionJson[z].Options[0].Value = customPackageName
	// 				}
	// 			}

	// 		}
	// 	}
	// }

	file, _ := json.Marshal(patchOptionJson)
	if err := os.WriteFile("patches/gorevancify-patch-options.json", file, 0666); err != nil {
		fmt.Print(err)
	}
	return nil
}

func setTableCellsLength() {
	//fmt.Printf("patchesNames len: %v \n", len(patchesNames))
	for i := 0; i < len(patchesNames); i++ {
		if nameLength < len(patchesNames[i]) {
			nameLength = len(patchesNames[i]) * 25
		}
		if descLength < len(patchesNames[i]) {
			descLength = len(patchesNames[i]) * 120
		}
	}
	//fmt.Printf("nameLen: %v \n descLen: %v", nameLength, descLength)
}

func prepareDict() {
	dict["com.crunchyroll.crunchyroid"] = "Crunchyroll"
	dict["com.google.android.youtube"] = "Youtube"
	dict["com.amazon.mShop.android.shopping"] = "Amazon Shopping"
	dict["tv.twitch.android.app"] = "Twitch"
	dict["com.google.android.apps.youtube.music"] = "Youtube Music"
	dict["it.ipzs.cieid"] = "CieID"
	dict["com.twitter.android"] = "Twitter"
	dict["com.spotify.music"] = "Spotify"
	dict["com.tumblr"] = "Tumblr"
	dict["com.laurencedawson.reddit_sync"] = "Sync for Reddit"
	dict["com.duolingo"] = "Duolingo"
	dict["com.myprog.hexedit"] = "HEX Editor"
	dict["com.rubenmayayo.reddit"] = "Boost for Reddit"
	dict["o.o.joey"] = "Joey for Reddit"
	dict["io.syncapps.lemmy_sync"] = "Sync for Lemmy"
	dict["com.ss.android.ugc.trill"] = "TikTok (Asia)"
	dict["com.adobe.lrmobile"] = "Lightroom"
	dict["com.reddit.frontpage"] = "Reddit"
	dict["com.strava"] = "Strava"
	dict["com.facebook.orca"] = "Messenger"
	dict["com.soundcloud.android"] = "SoundCloud"
	dict["com.piccomaeurope.fr"] = "Piccoma"
	dict["com.google.android.apps.magazines"] = "Google News"
	dict["com.spotify.lite"] = "Spotify Lite"
	dict["de.simon.openinghours"] = "Opening Hours"
	dict["com.xiaomi.wearable"] = "Mi Fitness"
	dict["com.google.android.apps.photos"] = "Google Photos"
	dict["com.facebook.katana"] = "Facebook"
	dict["com.nis.app"] = "Inshorts"
	dict["com.instagram.android"] = "Instagram"
	dict["com.myfitnesspal.android"] = "MyFitnessPal"
	dict["jp.pxv.android"] = "Pixiv"
	dict["at.willhaben"] = "Willhaben"
	dict["de.stocard.stocard"] = "Stocard"
	dict["com.rarlab.rar"] = "WinRAR"
	dict["com.microblink.photomath"] = "Photomath"
	dict["com.backdrops.wallpapers"] = "Backdrops Wallpapers"
	dict["de.dwd.warnapp"] = "WarnWetter"
	dict["com.swisssign.swissid.mobile"] = "SwissID"
	dict["net.binarymode.android.irplus"] = "Irplus - Infrared Remote"
	dict["com.sony.songpal.mdr"] = "Sony | Sound Connect"
	dict["at.gv.bmf.bmf2go"] = "FinanzOnline"
	dict["eu.faircode.netguard"] = "NetGuard - no-root firewall"
	dict["com.google.android.apps.recorder"] = "Recorder"
	dict["pl.solidexplorer2"] = "Solid Explorer File Manager"
	dict["com.bandcamp.android"] = "Bandcamp"
	dict["at.gv.oe.app"] = "Digitales Amt"
	dict["at.gv.bka.serviceportal"] = "SPB Serviceportal Bund"
	dict["de.tudortmund.app"] = "TU Dortmund"
	dict["com.onelouder.baconreader"] = "BaconReader for Reddit"
	dict["ml.docilealligator.infinityforreddit"] = "Infinity for Reddit"
	dict["com.andrewshu.android.reddit"] = "Rif is fun for Reddit"
	dict["free.reddit.news"] = "Relay for Reddit"
	dict["me.ccrama.redditslide"] = "Slide for Reddit"
	dict["io.yuka.android"] = "Yuka Food & Cosmetic Scanner"
	dict["ginlemon.iconpackstudio"] = "Icon Pack Studio"
	dict["com.zombodroid.MemeGenerator"] = "Meme Generator"
	dict["org.totschnig.myexpenses"] = "My Expenses"
	dict["com.wakdev.apps.nfctools.se"] = "NFC Tools"
	dict["tv.trakt.trakt"] = "Trakt"
	dict["co.windyapp.android"] = "Windy.app"
	dict["com.ticktick.task"] = "TickTick - Todo & Task List"
}

func unmarshalJson() {
	patchJsonFile := "patches.json"
	//fmt.Printf("patchJsonFile: %v \n", patchJsonFile)
	data, err := os.ReadFile(patchJsonFile)
	if err != nil {
		fmt.Printf("error patchJsonFile: %s\n", err)
		return
	}

	err = json.Unmarshal(data, &patches)
	if err != nil {
		return
	}
}

func processPatchData() ([]string, error) {

	clearTable()
	for _, patch := range patches {

		if len(patch.CompatiblePackages) > 0 {
			if patch.CompatiblePackages[0].Name == packageName {

				for _, version := range patch.CompatiblePackages[0].Versions {

					found := false
					for _, currentVersion := range supportedVersions {
						if currentVersion == version {
							found = true
							break
						}
					}
					if !found {
						supportedVersions = append(supportedVersions, version)
					}
				}

				patchesNames = append(patchesNames, patch.Name)
				desc = append(desc, patch.Description)
				include = append(include, patch.Use)
			}

		}
	}

	setTableCellsLength()

	return supportedVersions, nil
}

func loadPatchNames() *widget.Table {

	table := widget.NewTable(
		// Dimensiones de la tabla: tantas filas como nombres y 3 columnas.
		func() (int, int) {
			return len(patchesNames), 3
		},
		// Crear una celda vacía, se llenará más adelante.
		func() fyne.CanvasObject {
			return container.NewVBox() // Cambiado a un contenedor vertical.
		},
		// Llenar las celdas dinámicamente con datos y widgets.
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			row := id.Row
			container := cell.(*fyne.Container) // Asegurarse de que la celda es un contenedor.

			// Limpiar objetos anteriores del contenedor
			container.Objects = nil

			switch id.Col {
			case 0: // Columna 0: Checkbox
				check := widget.NewCheck("", func(checked bool) {
					include[row] = checked

					// Agregar a currentPatchesSelected si el checkbox está activado y el parche no está ya presente
					if checked {
						exists := false
						for _, name := range currentPatchesSelected {
							if name == patchesNames[row] {
								exists = true
								break
							}
						}
						if !exists {
							currentPatchesSelected = append(currentPatchesSelected, patchesNames[row])
						}
					} else {
						// Remover si el checkbox se desactiva
						for i, name := range currentPatchesSelected {
							if name == patchesNames[row] {
								currentPatchesSelected = append(currentPatchesSelected[:i], currentPatchesSelected[i+1:]...)
								break
							}
						}
					}

				})
				check.SetChecked(include[row])

				container.Add(check)

			case 1: // Columna 1: Nombre
				label := widget.NewLabel(patchesNames[row])
				container.Add(label)
			case 2: // Columna 2: Descripción
				label := widget.NewLabel(desc[row])
				container.Add(label)
			}

			// Ajustar el layout del contenedor
			container.Layout.Layout(container.Objects, container.Size())
		},
	)
	return table
}

func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}

func prepareOptionsAndPatchesJson(projName string) {
	os.Remove("options.json")
	//os.Remove("patches.json")

	latestPatch, err := getLatestPatchFile(projName)

	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	cmd := exec.Command("java", "-jar", cliSource, "options", latestPatch)
	executePatching(cmd)

	cmd = exec.Command("java", "-jar", cliSource, "patches", latestPatch)
	executePatching(cmd)
}

func getLatestPatchFile(projName string) (string, error) {
	dir := fmt.Sprintf("patches/%s", projName)
	var files []fs.FileInfo

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if entry.Type().IsRegular() && filepath.Ext(entry.Name()) == ".rvp" && filepath.HasPrefix(entry.Name(), "patches-") {
			info, err := entry.Info()
			if err == nil {
				files = append(files, info)
			}
		}
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no patch files found")
	}

	// Ordenar por fecha de modificación (más reciente al final)
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime().Before(files[j].ModTime())
	})

	latest := files[len(files)-1]
	return filepath.Join(dir, latest.Name()), nil
}

func getAvailableAppsNamesByPkg() {

	unmarshalJson()

	supportedAppMap := make(map[string]bool)

	for _, patch := range patches {
		for pkg, name := range dict {
			for _, compatible := range patch.CompatiblePackages {
				if pkg == compatible.Name {

					if !supportedAppMap[name] {
						supportedApp = append(supportedApp, name)
						supportedAppMap[name] = true
					}
				}
			}
		}
	}
	sort.Strings(supportedApp)
}

func getPackageNamesByAppName(appName string) string {

	for pkg, name := range dict {
		if name == appName {
			return pkg
		}
	}
	return "nil"
}

func readSettings() bool {
	data, err := os.ReadFile("settings.txt")
	if err != nil {
		return false
	}
	content := string(data)
	return content == "updateOnStart=true\n"
}

func main() {
	var a = app.New()
	var w = a.NewWindow("GoRevancify " + version)
	var appAPK string

	prepareDict()

	// console log
	logData.Set("")
	logLabel.Wrapping = fyne.TextWrapWord

	consoleLog.SetMinSize(fyne.NewSize(200, 500))
	consoleLog.Refresh()
	logLabel.Refresh()

	// Load projects from JSON file
	sources := loadSourcesFromFile("patches/sources.json")
	orgNames = getOrgNames(sources)

	//LoadSettings
	if readSettings() {
		updatePatches()
	}

	updateOnStart := widget.NewCheck("Update patches on start", func(checked bool) {
		// write settings.txt
		err := os.WriteFile("settings.txt", []byte(fmt.Sprintf("updateOnStart=%t\n", checked)), 0644)
		if err != nil {
			fmt.Println("Error writing the file:", err)
		}
	})
	updateOnStart.Checked = readSettings()
	// Dropdown versions
	var versionOptions []string
	dropdownVer := widget.NewSelect(versionOptions, func(selected string) {
		apkDownloadVersion = selected
	})
	dropdownVer.PlaceHolder = "Select version"
	dropdownVer.Alignment = fyne.TextAlignCenter

	// Dropdown App to patch
	patchName := widget.NewLabel("")
	var appOptions []string
	dropdownApp := widget.NewSelect(appOptions, func(selected string) {
		writePatchesTXT()
		appToPatch = selected

		dropdownVer.ClearSelected()
		dropdownVer.Options = []string{}
		dropdownVer.Refresh()

		packageName = getPackageNamesByAppName(selected)
		versions, err := processPatchData()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		loadSourcesFromFileOptions("options.json")

		patchChosen = selected

		dropdownVer.Options = versions
		dropdownVer.Refresh()
		for i := 0; i < len(patches); i++ {
			patchTable.SetRowHeight(i, 35)
		}
		patchTable.SetColumnWidth(1, float32(nameLength))
		patchTable.SetColumnWidth(2, float32(descLength))
		patchTable.Refresh()
		patchScroller.Refresh()

	})
	dropdownApp.PlaceHolder = "Select App"
	dropdownApp.Alignment = fyne.TextAlignCenter

	// Dropdown patches

	dropdown := widget.NewSelect(orgNames, func(selected string) {

		dropdownVer.ClearSelected()
		dropdownVer.Options = []string{}

		dropdownApp.Options = nil
		dropdownApp.ClearSelected()
		supportedApp = []string{}
		patchName.Text = "Patch selected: " + selected

		// Get patch data and available versions
		prepareOptionsAndPatchesJson(selected)

		getAvailableAppsNamesByPkg()
		dropdownApp.Options = supportedApp
		//dropdownApp.Refresh()

	})
	dropdown.PlaceHolder = "Select patch"
	dropdown.Alignment = fyne.TextAlignCenter

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Enter output name")

	var openApkFileButton *widget.Button
	openApkFileButton = widget.NewButton("Select APK... \n(Apk not selected)", func() {
		fd := dialog.NewFileOpen(func(file fyne.URIReadCloser, err error) {
			if err != nil || file == nil {
				return
			}
			appAPK = file.URI().String()
			selectedApkName := strings.Split(appAPK, "/")
			openApkFileButton.SetText("APK Selected \n(" + selectedApkName[len(selectedApkName)-1] + ")")
		}, w)
		fd.Resize(fyne.NewSize(800, 700))
		fd.Show()
	})

	downloadApkButton := widget.NewButton("Download (ApkMirror)", func() {
		if patchChosen == "" {
			dialog.ShowInformation("Error", "Patch not chosen", w)
			return
		}
		url := fmt.Sprint("https://www.apkmirror.com/?post_type=app_release&searchtype=apk&bundles%5B%5D=apkm_bundles&bundles%5B%5D=apk_files&s=" + appToPatch + " " + apkDownloadVersion)
		openBrowser(url)

	})

	apkPartLabel := widget.NewLabel("\nOR...")
	apkPartLabel.Alignment = fyne.TextAlignCenter
	apkPartLabel.TextStyle = fyne.TextStyle{Bold: true}
	apkPart := container.New(&horizontalCustomLayout{
		widths:  []float32{376, 50, 376},
		heights: []float32{100, 100, 100},
		tabbing: []float32{0, 0, 0},
	}, openApkFileButton, apkPartLabel, downloadApkButton)

	patchButton := widget.NewButton("Patch APK", func() {
		if patching {
			dialog.ShowCustom("error", "close", widget.NewLabel("Already patching"), w)
			return
		}
		if patchName.Text == "" {
			dialog.ShowCustom("error", "close", widget.NewLabel("Patch not selected"), w)
			return
		}
		patch := strings.Split(patchName.Text, " ")[2]

		patchesSource := "patches/" + patch + "/patches-*.rvp"

		if appAPK == "" {
			dialog.ShowInformation("Error", "No APK selected!", w)
			return
		} else if nameEntry.Text == "" || nameEntry.Text == "Enter output name" {
			dialog.ShowInformation("Error", "Name not valid", w)
			return
		} else {

			go func() {
				//fmt.Println("patchesJson: " + patchesJson)
				err := PatchApp(appAPK, cliSource, patch, nameEntry.Text, patchesSource, logData, w)
				if err != nil {
					dialog.ShowError(err, w)
				} else {
					dialog.ShowInformation("Success", "APK patched successfully! \n"+fmt.Sprintf("apps/patched/%s-patched-%s.apk", nameEntry.Text, patch), w)
				}
			}()
		}
	})

	patchAndConsole := container.New(&verticalCustomLayout{
		widths:  []float32{800, 50, 800},
		heights: []float32{100, 50, 300},
	}, patchButton, widget.NewLabel("Console Log"), consoleLog)

	nameEntry.Resize(fyne.NewSize(100, 50))

	patchPart := container.New(&horizontalCustomLayout{
		widths:  []float32{260, 260, 260},
		heights: []float32{50, 50, 50},
		tabbing: []float32{5, 5, 0},
	}, dropdown, dropdownApp, dropdownVer)

	form := container.NewVBox(
		updateOnStart,
		widget.NewLabel(""),
		patchPart,
		apkPart,
		widget.NewLabel(""),
		container.New(&horizontalCustomLayout{
			widths:  []float32{800},
			heights: []float32{40},
			tabbing: []float32{0},
		}, nameEntry),
		patchAndConsole,
	)

	patchTable.SetColumnWidth(0, 30)

	patchScroller = container.NewVScroll(patchTable)
	patchScroller.SetMinSize(fyne.NewSize(100, 500))
	patchScroller.Refresh()

	appName := widget.NewEntry()
	appName.SetPlaceHolder("Enter app name.. Default: Youtube")
	pkgName := widget.NewEntry()
	pkgName.SetPlaceHolder("Enter pkg name.. Default: " + customPackageName)

	selectAllOptions := widget.NewButton("Select All", func() {
		for i := 0; i < len(patchesNames); i++ {
			include[i] = true
			id := widget.TableCellID{Row: i, Col: 0} // Set the include state to true for all patches
			patchTable.Select(id)
			currentPatchesSelected = append(currentPatchesSelected, patchesNames[i]) // Select the checkbox in the first column
		}
	})

	unselectAllOptions := widget.NewButton("Unselect All", func() {
		for i := 0; i < len(patchesNames); i++ {
			include[i] = false
			id := widget.TableCellID{Row: i, Col: 0} // Set the include state to false for all patches
			patchTable.Unselect(id)                  // Unselect the checkbox in the first column
			currentPatchesSelected = append(currentPatchesSelected, patchesNames[i])
		}

		currentPatchesSelected = []string{}
	})

	patchOptionsTab := container.NewVBox(
		patchName,
		widget.NewLabelWithStyle("Patch manager", fyne.TextAlign(fyne.TextAlignCenter), fyne.TextStyle{Bold: true, TabWidth: 5}),
		container.New(&horizontalCustomLayout{
			widths:  []float32{450, 450},
			heights: []float32{50, 50},
			tabbing: []float32{10, 0},
		},
			selectAllOptions, unselectAllOptions),
		patchScroller,
		widget.NewLabel(""),
		container.New(&horizontalCustomLayout{
			widths:  []float32{450, 450},
			heights: []float32{50, 50},
			tabbing: []float32{5, 0},
		},
			container.NewVBox(
				widget.NewLabel("App name"),
				appName),
			container.NewVBox(
				widget.NewLabel("Package name"),
				pkgName)),
		widget.NewButton("Save changes", func() {
			if appName.Text != appName.PlaceHolder {
				AppName = appName.Text
			}
			if pkgName.Text != pkgName.PlaceHolder {
				customPackageName = pkgName.Text
			}
			writePatchesTXT()
			dialog.ShowInformation("Information", "Changes saved", w)
		}),
	)

	app := container.NewAppTabs(
		container.NewTabItem("Patching", form),
		container.NewTabItem("Patch options", patchOptionsTab),
	)

	w.SetContent(app)
	w.Resize(fyne.NewSize(800, 650))
	w.ShowAndRun()
}

func getCurrentFileLocation() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get main.go location")
	}
	mainDir := filepath.Dir(filename)
	return mainDir, nil
}

func OpenFileManager() error {
	var cmd *exec.Cmd
	path, _ := getCurrentFileLocation()
	path += "\\apps\\patched"

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)

	case "darwin":
		cmd = exec.Command("open", path)

	case "linux":
		cmd = exec.Command("xdg-open", path)

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to open file manager: %w", err)
	}

	return nil
}

func loadSourcesFromFile(filename string) map[string]Source {
	file, err := os.ReadFile(filename)
	if err != nil {
		fmt.Println("Error reading sources file:", err)
		return nil
	}

	var sources map[string]Source
	if err := json.Unmarshal(file, &sources); err != nil {
		fmt.Println("Error unmarshalling sources JSON:", err)
		return nil
	}
	return sources
}

func loadSourcesFromFileOptions(filename string) map[string]PatchOptionsJSON {
	file, _ := os.ReadFile(filename)
	fmt.Printf("file: %s\n", filename)
	var options map[string]PatchOptionsJSON
	//fmt.Println("patchOptionJson:", patchOptionJson)
	if err := json.Unmarshal(file, &patchOptionJson); err != nil {
		fmt.Println("Error unmarshalling PatchOptionsJSON:", err)
		return nil
	}

	return options
}

func executePatching(cmd *exec.Cmd) error {
	if err := cmd.Run(); err != nil {
		logError(err)
		return fmt.Errorf("error running patch command: %v", err)
	}
	return nil
}

func checkPatchPreRequisites(appName, apk string, w fyne.Window) bool {
	if appName == "" {
		dialog.ShowCustom("error", "close", widget.NewLabel("Invalid app name"), w)
		return false
	}
	if apk == "Apk not selected..." {
		dialog.ShowCustom("error", "close", widget.NewLabel("Apk not selected"), w)
		return false
	}
	return true
}

func PatchApp(apk, cliSource, source, appName, patchesSource string, logData binding.String, w fyne.Window) error {

	if !checkPatchPreRequisites(appName, apk, w) {
		return nil
	}

	// Open the file for reading
	file, err := os.Open(patchOptionsPath)
	if err != nil {
		return fmt.Errorf("error opening file: %v", err)
	}
	defer file.Close()

	apk = strings.Split(apk, "file://")[1]

	outputPath := fmt.Sprintf("apps/patched/%s-patched-%s-%v.apk", appName, source, version)
	patching = true

	//Include patches
	var patchArgs []string
	for _, patch := range currentPatchesSelected {
		//patchArgs = append(patchArgs, "-e", fmt.Sprintf("\"%s\"", patch))
		patchArgs = append(patchArgs, "-e", patch)
	}
	patchesSourceSlice := strings.Split(patchesSource, "/")

	cmdArgs := []string{
		"-jar", cliSource, "patch",
		apk,
		"--patches", "patches/" + patchesSourceSlice[1] + "/patches-*.rvp",
		"--out", outputPath,
		"-O", "patches/gorevancify-patch-options.json",
		"--exclusive",
	}
	cmdArgs = append(cmdArgs, patchArgs...)
	cmd := exec.Command("java", cmdArgs...)

	writeLogs(cmd, logData)
	executePatching(cmd)

	deleteTempFiles(appName, source)

	// verify if apk patched succesfully
	if _, err := os.Stat(fmt.Sprintf("apps/patched/%s-patched-%s-%v.apk", appName, source, version)); os.IsNotExist(err) {
		patching = false
		return fmt.Errorf("patching failed")
	} else {
		OpenFileManager()
	}
	patching = false

	return nil
}

func deleteTempFiles(appName, source string) {

	filepath := fmt.Sprintf("apps/patched/%s-patched-%s-%v-temporary-files", appName, source, version)
	addLogText("REMOVING: " + filepath)

	if err := os.RemoveAll(filepath); err != nil {
		addLogText("error removing folder" + filepath + ": " + err.Error())
	} else {
		addLogText("Folder removed successfully.")
	}
	filepath = fmt.Sprintf("apps/patched/%s-patched-%s-%v.keystore", appName, source, version)
	addLogText("REMOVING: " + filepath)
	os.Remove(filepath)
	if err := os.RemoveAll(filepath); err != nil {
		addLogText("error removing folder" + filepath + ": " + err.Error())
	} else {
		addLogText("Folder removed successfully.")
	}

	filepath = "revancify.keystore"
	addLogText("REMOVING: " + filepath)
	os.Remove(filepath)
	if err := os.RemoveAll(filepath); err != nil {
		addLogText("error removing folder" + filepath + ": " + err.Error())
	} else {
		addLogText("Folder removed successfully.")
	}

	filepath = "revx.keystore"
	addLogText("REMOVING: " + filepath)
	os.Remove(filepath)
	if err := os.RemoveAll(filepath); err != nil {
		addLogText("error removing folder" + filepath + ": " + err.Error())
	} else {
		addLogText("Folder removed successfully.")
	}
}

func writeLogs(cmd *exec.Cmd, logData binding.String) error {

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error creating stdout pipe: %v", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("error creating stderr pipe: %v", err)
	}
	// Show cli output
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			currentLog, _ := logData.Get()
			newLog := currentLog + fmt.Sprintf(" %s\n", scanner.Text())

			logData.Set(newLog)
			consoleLog.ScrollToBottom()
		}
	}()

	// Show cli error
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			currentLog, _ := logData.Get()
			newLog := currentLog + fmt.Sprintf(" %s\n", scanner.Text())
			logData.Set(newLog)
			consoleLog.ScrollToBottom()
		}
	}()
	return nil
}
func addLogText(text string) {
	currentLog, _ := logData.Get()
	newLog := currentLog + fmt.Sprintf(" %s\n", text)
	logData.Set(newLog)
	consoleLog.ScrollToBottom()
}

func logError(err error) {

	f, fileErr := os.OpenFile("logs/error_log.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if fileErr != nil {
		fmt.Println("Error opening log:", fileErr)
		return
	}
	defer f.Close()

	if _, fileErr = f.WriteString(fmt.Sprintf("[%s] %v\n", time.Now().Format(time.RFC3339), err)); fileErr != nil {
		fmt.Println("Error writing log:", fileErr)
	}
}

func getLatestReleaseURL(org, repo string) (string, string, error) {

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", org, repo)
	resp, err := http.Get(url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var data struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", err
	}

	for _, asset := range data.Assets {
		if strings.HasSuffix(asset.Name, ".rvp") {
			return asset.BrowserDownloadURL, data.TagName, nil
		}
	}

	return "", "", fmt.Errorf("no .rvp asset found in latest release")
}

func updatePatches() {
	for _, org := range orgNames {
		dirPath := "patches/" + org + "/"

		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				fmt.Println("Error creating folder:", err)
				continue
			}
		}

		downloadURL, version, err := getLatestReleaseURL(org, "revanced-patches")
		if err != nil {
			fmt.Println("Error getting latest release for", org, ":", err)
			continue
		}

		dest := filepath.Join(dirPath, "patches-"+version+".rvp")

		// Descargar si no existe
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			fmt.Println("Downloading latest patch:", downloadURL)
			if err := DownloadFile(dest, downloadURL); err != nil {
				fmt.Println("Download error:", err)
			} else {
				fmt.Println("Downloaded:", dest)
			}
		} else {
			fmt.Println("Latest patch already exists:", dest)
		}
	}
}

func DownloadFile(filepath string, url string) error {
	fmt.Println("Downloading from:", url)
	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}
