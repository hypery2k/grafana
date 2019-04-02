package dashboards

import (
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/dashboards"

	"github.com/grafana/grafana/pkg/log"
	. "github.com/smartystreets/goconvey/convey"
)

var (
	defaultDashboards = "testdata/test-dashboards/folder-one"
	brokenDashboards  = "testdata/test-dashboards/broken-dashboards"
	oneDashboard      = "testdata/test-dashboards/one-dashboard"
	containingId      = "testdata/test-dashboards/containing-id"
	unprovision       = "testdata/test-dashboards/unprovision"

	fakeService *fakeDashboardProvisioningService
)

func TestCreatingNewDashboardFileReader(t *testing.T) {
	Convey("creating new dashboard file reader", t, func() {
		cfg := &DashboardsAsConfig{
			Name:    "Default",
			Type:    "file",
			OrgId:   1,
			Folder:  "",
			Options: map[string]interface{}{},
		}

		Convey("using path parameter", func() {
			cfg.Options["path"] = defaultDashboards
			reader, err := NewDashboardFileReader(cfg, log.New("test-logger"))
			So(err, ShouldBeNil)
			So(reader.Path, ShouldNotEqual, "")
		})

		Convey("using folder as options", func() {
			cfg.Options["folder"] = defaultDashboards
			reader, err := NewDashboardFileReader(cfg, log.New("test-logger"))
			So(err, ShouldBeNil)
			So(reader.Path, ShouldNotEqual, "")
		})

		Convey("using full path", func() {
			fullPath := "/var/lib/grafana/dashboards"
			if runtime.GOOS == "windows" {
				fullPath = `c:\var\lib\grafana`
			}

			cfg.Options["folder"] = fullPath
			reader, err := NewDashboardFileReader(cfg, log.New("test-logger"))
			So(err, ShouldBeNil)

			So(reader.Path, ShouldEqual, fullPath)
			So(filepath.IsAbs(reader.Path), ShouldBeTrue)
		})

		Convey("using relative path", func() {
			cfg.Options["folder"] = defaultDashboards
			reader, err := NewDashboardFileReader(cfg, log.New("test-logger"))
			So(err, ShouldBeNil)

			resolvedPath := reader.resolvePath(reader.Path)
			So(filepath.IsAbs(resolvedPath), ShouldBeTrue)
		})
	})
}

func TestDashboardFileReader(t *testing.T) {
	Convey("Dashboard file reader", t, func() {
		bus.ClearBusHandlers()
		origNewDashboardProvisioningService := dashboards.NewProvisioningService
		fakeService = mockDashboardProvisioningService()

		bus.AddHandler("test", mockGetDashboardQuery)
		logger := log.New("test.logger")

		Convey("Reading dashboards from disk", func() {

			cfg := &DashboardsAsConfig{
				Name:    "Default",
				Type:    "file",
				OrgId:   1,
				Folder:  "",
				Options: map[string]interface{}{},
			}

			Convey("Can read default dashboard", func() {
				cfg.Options["path"] = defaultDashboards
				cfg.Folder = "Team A"

				reader, err := NewDashboardFileReader(cfg, logger)
				So(err, ShouldBeNil)

				err = reader.startWalkingDisk()
				So(err, ShouldBeNil)

				folders := 0
				dashboards := 0

				for _, i := range fakeService.inserted {
					if i.Dashboard.IsFolder {
						folders++
					} else {
						dashboards++
					}
				}

				So(folders, ShouldEqual, 1)
				So(dashboards, ShouldEqual, 2)
			})

			Convey("Can read default dashboard and replace old version in database", func() {
				cfg.Options["path"] = oneDashboard

				stat, _ := os.Stat(oneDashboard + "/dashboard1.json")

				fakeService.getDashboard = append(fakeService.getDashboard, &models.Dashboard{
					Updated: stat.ModTime().AddDate(0, 0, -1),
					Slug:    "grafana",
				})

				reader, err := NewDashboardFileReader(cfg, logger)
				So(err, ShouldBeNil)

				err = reader.startWalkingDisk()
				So(err, ShouldBeNil)

				So(len(fakeService.inserted), ShouldEqual, 1)
			})

			Convey("Overrides id from dashboard.json files", func() {
				cfg.Options["path"] = containingId

				reader, err := NewDashboardFileReader(cfg, logger)
				So(err, ShouldBeNil)

				err = reader.startWalkingDisk()
				So(err, ShouldBeNil)

				So(len(fakeService.inserted), ShouldEqual, 1)
			})

			Convey("Invalid configuration should return error", func() {
				cfg := &DashboardsAsConfig{
					Name:   "Default",
					Type:   "file",
					OrgId:  1,
					Folder: "",
				}

				_, err := NewDashboardFileReader(cfg, logger)
				So(err, ShouldNotBeNil)
			})

			Convey("Broken dashboards should not cause error", func() {
				cfg.Options["path"] = brokenDashboards

				_, err := NewDashboardFileReader(cfg, logger)
				So(err, ShouldBeNil)
			})

			Convey("Two dashboard providers should be able to provisioned the same dashboard without uid", func() {
				cfg1 := &DashboardsAsConfig{Name: "1", Type: "file", OrgId: 1, Folder: "f1", Options: map[string]interface{}{"path": containingId}}
				cfg2 := &DashboardsAsConfig{Name: "2", Type: "file", OrgId: 1, Folder: "f2", Options: map[string]interface{}{"path": containingId}}

				reader1, err := NewDashboardFileReader(cfg1, logger)
				So(err, ShouldBeNil)

				err = reader1.startWalkingDisk()
				So(err, ShouldBeNil)

				reader2, err := NewDashboardFileReader(cfg2, logger)
				So(err, ShouldBeNil)

				err = reader2.startWalkingDisk()
				So(err, ShouldBeNil)

				var folderCount int
				var dashCount int
				for _, o := range fakeService.inserted {
					if o.Dashboard.IsFolder {
						folderCount++
					} else {
						dashCount++
					}
				}

				So(folderCount, ShouldEqual, 2)
				So(dashCount, ShouldEqual, 2)
			})
		})

		Convey("Should not create new folder if folder name is missing", func() {
			cfg := &DashboardsAsConfig{
				Name:   "Default",
				Type:   "file",
				OrgId:  1,
				Folder: "",
				Options: map[string]interface{}{
					"folder": defaultDashboards,
				},
			}

			_, err := getOrCreateFolderId(cfg, fakeService)
			So(err, ShouldEqual, ErrFolderNameMissing)
		})

		Convey("can get or Create dashboard folder", func() {
			cfg := &DashboardsAsConfig{
				Name:   "Default",
				Type:   "file",
				OrgId:  1,
				Folder: "TEAM A",
				Options: map[string]interface{}{
					"folder": defaultDashboards,
				},
			}

			folderId, err := getOrCreateFolderId(cfg, fakeService)
			So(err, ShouldBeNil)
			inserted := false
			for _, d := range fakeService.inserted {
				if d.Dashboard.IsFolder && d.Dashboard.Id == folderId {
					inserted = true
				}
			}
			So(len(fakeService.inserted), ShouldEqual, 1)
			So(inserted, ShouldBeTrue)
		})

		Convey("Walking the folder with dashboards", func() {
			noFiles := map[string]os.FileInfo{}

			Convey("should skip dirs that starts with .", func() {
				shouldSkip := createWalkFn(noFiles, nil)("path", &FakeFileInfo{isDirectory: true, name: ".folder"}, nil)
				So(shouldSkip, ShouldEqual, filepath.SkipDir)
			})

			Convey("should keep walking if file is not .json", func() {
				shouldSkip := createWalkFn(noFiles, nil)("path", &FakeFileInfo{isDirectory: true, name: "folder"}, nil)
				So(shouldSkip, ShouldBeNil)
			})
		})

		Convey("Should unprovision missing dashboard if preventDelete = true", func() {
			cfg := &DashboardsAsConfig{
				Name:  "Default",
				Type:  "file",
				OrgId: 1,
				Options: map[string]interface{}{
					"folder": unprovision,
				},
				DisableDeletion: true,
			}

			reader, err := NewDashboardFileReader(cfg, logger)
			So(err, ShouldBeNil)

			err = reader.startWalkingDisk()
			So(err, ShouldBeNil)

			So(len(fakeService.provisioned["Default"]), ShouldEqual, 2)
			So(len(fakeService.inserted), ShouldEqual, 2)

			reader.fileFilterFunc = func(fileInfo os.FileInfo) bool {
				return fileInfo.Name() == "dashboard1.json"
			}

			err = reader.startWalkingDisk()
			So(err, ShouldBeNil)

			So(len(fakeService.provisioned["Default"]), ShouldEqual, 1)
			So(len(fakeService.inserted), ShouldEqual, 2)
		})

		Reset(func() {
			dashboards.NewProvisioningService = origNewDashboardProvisioningService
		})
	})
}

type FakeFileInfo struct {
	isDirectory bool
	name        string
}

func (ffi *FakeFileInfo) IsDir() bool {
	return ffi.isDirectory
}

func (ffi FakeFileInfo) Size() int64 {
	return 1
}

func (ffi FakeFileInfo) Mode() os.FileMode {
	return 0777
}

func (ffi FakeFileInfo) Name() string {
	return ffi.name
}

func (ffi FakeFileInfo) ModTime() time.Time {
	return time.Time{}
}

func (ffi FakeFileInfo) Sys() interface{} {
	return nil
}

func mockDashboardProvisioningService() *fakeDashboardProvisioningService {
	mock := fakeDashboardProvisioningService{
		provisioned: map[string][]*models.DashboardProvisioning{},
	}
	dashboards.NewProvisioningService = func() dashboards.DashboardProvisioningService {
		return &mock
	}
	return &mock
}

type fakeDashboardProvisioningService struct {
	inserted     []*dashboards.SaveDashboardDTO
	provisioned  map[string][]*models.DashboardProvisioning
	getDashboard []*models.Dashboard
}

func (s *fakeDashboardProvisioningService) GetProvisionedDashboardData(name string) ([]*models.DashboardProvisioning, error) {
	if _, ok := s.provisioned[name]; !ok {
		s.provisioned[name] = []*models.DashboardProvisioning{}
	}

	return s.provisioned[name], nil
}

func (s *fakeDashboardProvisioningService) SaveProvisionedDashboard(dto *dashboards.SaveDashboardDTO, provisioning *models.DashboardProvisioning) (*models.Dashboard, error) {
	s.inserted = append(s.inserted, dto)

	if _, ok := s.provisioned[provisioning.Name]; !ok {
		s.provisioned[provisioning.Name] = []*models.DashboardProvisioning{}
	}

	// Copy the struct as we need to assign some dashboardId to it but do not want to alter outside world.
	var copyProvisioning = &models.DashboardProvisioning{}
	*copyProvisioning = *provisioning

	copyProvisioning.DashboardId = rand.Int63n(1000000)

	s.provisioned[provisioning.Name] = append(s.provisioned[provisioning.Name], copyProvisioning)
	return dto.Dashboard, nil
}

func (s *fakeDashboardProvisioningService) SaveFolderForProvisionedDashboards(dto *dashboards.SaveDashboardDTO) (*models.Dashboard, error) {
	s.inserted = append(s.inserted, dto)
	return dto.Dashboard, nil
}

func (s *fakeDashboardProvisioningService) UnprovisionDashboard(dashboardId int64) error {
	for key, val := range s.provisioned {
		for index, dashboard := range val {
			if dashboard.DashboardId == dashboardId {
				s.provisioned[key] = append(s.provisioned[key][:index], s.provisioned[key][index+1:]...)
			}
		}
	}
	return nil
}

func (s *fakeDashboardProvisioningService) DeleteProvisionedDashboard(dashboardId int64, orgId int64) error {
	panic("Should not be called in this test at the moment")
	return nil
}

func mockGetDashboardQuery(cmd *models.GetDashboardQuery) error {
	for _, d := range fakeService.getDashboard {
		if d.Slug == cmd.Slug {
			cmd.Result = d
			return nil
		}
	}

	return models.ErrDashboardNotFound
}
