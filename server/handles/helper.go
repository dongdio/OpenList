package handles

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gogf/gf/v2/net/ghttp"

	"github.com/dongdio/OpenList/consts"
	"github.com/dongdio/OpenList/internal/setting"
	"github.com/dongdio/OpenList/server/common"
	"github.com/dongdio/OpenList/utility/utils"
)

// Favicon handles the favicon request by redirecting to the configured favicon URL
func Favicon(c *gin.Context) {
	c.Redirect(302, setting.GetStr(consts.Favicon))
}

func Favicon1(c *ghttp.Request) {
	c.Response.RedirectTo(setting.GetStr(consts.Favicon), 302)
}

// Robots returns the configured robots.txt content
func Robots(c *gin.Context) {
	c.String(200, setting.GetStr(consts.RobotsTxt))
}

func Robots1(c *ghttp.Request) {
	c.Response.WriteStatus(200, setting.GetStr(consts.RobotsTxt))
}

// Plist generates an iOS plist file for app installation
// The link_name parameter is expected to be a base64 encoded string containing URL and name information
func Plist(c *gin.Context) {
	// Extract and decode the link name from the URL parameter
	linkNameB64 := strings.TrimSuffix(c.Param("link_name"), ".plist")
	linkName, err := utils.SafeAtob(linkNameB64)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// Split the link name into URL and name parts
	linkNameParts := strings.Split(linkName, "/")
	if len(linkNameParts) != 2 {
		common.ErrorStrResp(c, "malformed link", 400)
		return
	}

	// Process the URL part
	linkEncoded := linkNameParts[0]
	linkStr, err := url.PathUnescape(linkEncoded)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	link, err := url.Parse(linkStr)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// Process the name part
	nameEncoded := linkNameParts[1]
	fullName, err := url.PathUnescape(nameEncoded)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}

	// Sanitize URL and name
	downloadURL := link.String()
	downloadURL = sanitizeForXML(downloadURL)

	// Extract identifier from name if it contains separator
	name := fullName
	identifier := fmt.Sprintf("ci.nn.%s", url.PathEscape(fullName))

	const identifierSeparator = "@"
	if strings.Contains(fullName, identifierSeparator) {
		parts := strings.Split(fullName, identifierSeparator)
		name = strings.Join(parts[:len(parts)-1], identifierSeparator)
		identifier = parts[len(parts)-1]
	}

	name = sanitizeForXML(name)

	// Generate the plist XML content
	plist := generatePlistXML(downloadURL, identifier, name)

	// Return the plist as XML
	c.Header("Content-Type", "application/xml;charset=utf-8")
	c.Status(200)
	_, _ = c.Writer.WriteString(plist)
}

func Plist1(c *ghttp.Request) {
	// Extract and decode the link name from the URL parameter
	linkNameB64 := strings.TrimSuffix(c.GetRouter("link_name").String(), ".plist")
	linkName, err := utils.SafeAtob(linkNameB64)
	if err != nil {
		c.Response.WriteStatus(400, err.Error())
		return
	}

	// Split the link name into URL and name parts
	linkNameParts := strings.Split(linkName, "/")
	if len(linkNameParts) != 2 {
		c.Response.WriteStatus(400, "malformed link")
		return
	}

	// Process the URL part
	linkEncoded := linkNameParts[0]
	linkStr, err := url.PathUnescape(linkEncoded)
	if err != nil {
		c.Response.WriteStatus(400, err.Error())
		return
	}

	link, err := url.Parse(linkStr)
	if err != nil {
		c.Response.WriteStatus(400, err.Error())
		return
	}

	// Process the name part
	nameEncoded := linkNameParts[1]
	fullName, err := url.PathUnescape(nameEncoded)
	if err != nil {
		c.Response.WriteStatus(400, err.Error())
		return
	}

	// Sanitize URL and name
	downloadURL := link.String()
	downloadURL = sanitizeForXML(downloadURL)

	// Extract identifier from name if it contains separator
	name := fullName
	identifier := fmt.Sprintf("ci.nn.%s", url.PathEscape(fullName))

	const identifierSeparator = "@"
	if strings.Contains(fullName, identifierSeparator) {
		parts := strings.Split(fullName, identifierSeparator)
		name = strings.Join(parts[:len(parts)-1], identifierSeparator)
		identifier = parts[len(parts)-1]
	}
	name = sanitizeForXML(name)

	// Generate the plist XML content
	plist := generatePlistXML(downloadURL, identifier, name)

	// Return the plist as XML
	c.Response.Header().Add("Content-Type", "application/xml;charset=utf-8")
	c.Response.Write(plist)
}

// sanitizeForXML replaces characters that could cause issues in XML
func sanitizeForXML(input string) string {
	result := strings.ReplaceAll(input, "<", "[")
	return strings.ReplaceAll(result, ">", "]")
}

// generatePlistXML creates the XML content for the iOS app installation plist
func generatePlistXML(url, identifier, name string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
    <dict>
        <key>items</key>
        <array>
            <dict>
                <key>assets</key>
                <array>
                    <dict>
                        <key>kind</key>
                        <string>software-package</string>
                        <key>url</key>
                        <string><![CDATA[%s]]></string>
                    </dict>
                </array>
                <key>metadata</key>
                <dict>
                    <key>bundle-identifier</key>
					<string>%s</string>
					<key>bundle-version</key>
                    <string>4.4</string>
                    <key>kind</key>
                    <string>software</string>
                    <key>title</key>
                    <string>%s</string>
                </dict>
            </dict>
        </array>
    </dict>
</plist>`, url, identifier, name)
}