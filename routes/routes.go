package routes

import "github.com/tedsuo/rata"

const (
	ExecuteBuild     = "ExecuteBuild"
	AbortBuild       = "AbortBuild"
	CheckInput       = "CheckInput"
	CheckInputStream = "CheckInputStream"
)

var Routes = rata.Routes{
	{Path: "/builds", Method: "POST", Name: ExecuteBuild},
	{Path: "/builds/:guid/abort", Method: "POST", Name: AbortBuild},
	{Path: "/checks", Method: "POST", Name: CheckInput},
	{Path: "/checks/stream", Method: "GET", Name: CheckInputStream},
}
