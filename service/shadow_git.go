package service

// shadow_git.go — 影子代码库物化器：把抽取的文件操作写入裸 git 仓库。
//
// 自研松散对象写入（blob/tree/commit + refs），零外部依赖、容器内无需
// git 二进制；生成的仓库可被 git clone / log / archive 直接使用。
// 仓库布局：{baseDir}/{用户名}/{项目名}.git

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var (
	shadowRepoDir     string // 根目录，InitShadowRepoDir 设置
	shadowRepoDirOnce sync.Once
	shadowRepoLocks   sync.Map // repoKey → *sync.Mutex
)

// ShadowRepoBaseDir 影子仓库根目录（env SHADOW_REPO_DIR，默认 /data/shadow-repos）
func ShadowRepoBaseDir() string {
	shadowRepoDirOnce.Do(func() {
		dir := strings.TrimSpace(os.Getenv("SHADOW_REPO_DIR"))
		if dir == "" {
			dir = "/data/shadow-repos"
		}
		shadowRepoDir = dir
	})
	return shadowRepoDir
}

func repoLock(key string) *sync.Mutex {
	v, _ := shadowRepoLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// ---------- git 对象底层 ----------

func writeLooseObject(repoDir, objType string, payload []byte) (string, error) {
	header := []byte(fmt.Sprintf("%s %d\x00", objType, len(payload)))
	full := append(header, payload...)
	sum := sha1.Sum(full)
	sha := hex.EncodeToString(sum[:])

	objDir := filepath.Join(repoDir, "objects", sha[:2])
	if err := os.MkdirAll(objDir, 0o755); err != nil {
		return "", err
	}
	objPath := filepath.Join(objDir, sha[2:])
	if _, err := os.Stat(objPath); err == nil {
		return sha, nil // 已存在（内容相同）
	}
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(full); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := os.WriteFile(objPath, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	return sha, nil
}

// buildTree 递归构建 tree 对象，返回 tree sha
func buildTree(repoDir string, files map[string]string) (string, error) {
	// 路径 → sha（blob）
	type treeNode struct {
		name     string
		sha      string
		children map[string]*treeNode // dir
	}
	root := &treeNode{children: map[string]*treeNode{}}
	for path, content := range files {
		blobSha, err := writeLooseObject(repoDir, "blob", []byte(content))
		if err != nil {
			return "", err
		}
		segs := strings.Split(path, "/")
		cur := root
		for _, seg := range segs[:len(segs)-1] {
			next, ok := cur.children[seg]
			if !ok {
				next = &treeNode{children: map[string]*treeNode{}}
				cur.children[seg] = next
			}
			cur = next
		}
		name := segs[len(segs)-1]
		cur.children[name] = &treeNode{name: name, sha: blobSha}
	}
	var serialize func(n *treeNode) (string, error)
	serialize = func(n *treeNode) (string, error) {
		var buf bytes.Buffer
		names := make([]string, 0, len(n.children))
		for name := range n.children {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			child := n.children[name]
			if child.sha != "" {
				// 文件：100644
				fmt.Fprintf(&buf, "100644 %s\x00", name)
				raw, _ := hex.DecodeString(child.sha)
				buf.Write(raw)
			} else {
				// 目录：40000
				subSha, err := serialize(child)
				if err != nil {
					return "", err
				}
				fmt.Fprintf(&buf, "40000 %s\x00", name)
				raw, _ := hex.DecodeString(subSha)
				buf.Write(raw)
			}
		}
		return writeLooseObject(repoDir, "tree", buf.Bytes())
	}
	return serialize(root)
}

// gitUserFor 用户名 → git author
func gitUserFor(username string) (name, email string) {
	if username == "" {
		username = "unknown"
	}
	email = "shadow@erke.local"
	return username, email
}

// commitToRepo 向裸仓库写入一次 commit。files 为该仓库当前全量文件内容。
func commitToRepo(repoDir, username, message string, files map[string]string) error {
	treeSha, err := buildTree(repoDir, files)
	if err != nil {
		return err
	}
	// 读旧 head
	parentLine := ""
	headPath := filepath.Join(repoDir, "refs", "heads", "main")
	if old, err := os.ReadFile(headPath); err == nil {
		parentLine = fmt.Sprintf("parent %s\n", strings.TrimSpace(string(old)))
	}
	name, email := gitUserFor(username)
	ts := time.Now().Unix()
	zone := time.Now().Format("-0700")
	commit := fmt.Sprintf("tree %s\n%sauthor %s <%s> %s %s\ncommitter %s <%s> %s %s\n\n%s\n",
		treeSha, parentLine, name, email, strconv.FormatInt(ts, 10), zone,
		name, email, strconv.FormatInt(ts, 10), zone, message)
	commitSha, err := writeLooseObject(repoDir, "commit", []byte(commit))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "refs", "heads"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(headPath, []byte(commitSha+"\n"), 0o644); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(repoDir, "HEAD")); err != nil {
		os.WriteFile(filepath.Join(repoDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	}
	return nil
}

// ---------- 物化主流程 ----------

// ProcessShadowExtracts 物化器：处理未消费的抽取记录。
// 策略：按 (user, project) 分组，每组取每文件最新内容，整体重建 tree 提交。
// 仓库内文件全集 = 历史所有文件的最新内容（本批之外的历史内容从上一轮
// commit 的 tree 继承 —— v1 简化：从 DB 按 (user,project) 重建全量）。
func ProcessShadowExtracts() {
	if !common.ShadowRepoEnabled {
		return
	}
	rows, err := model.PendingFileExtracts(2000)
	if err != nil || len(rows) == 0 {
		return
	}
	// 1. 本批涉及的 (user, project)
	type repoKey struct {
		userId   int
		username string
		project  string
	}
	touched := map[repoKey]struct{}{}
	for _, r := range rows {
		touched[repoKey{r.UserId, r.Username, r.ProjectName}] = struct{}{}
	}
	ids := make([]int, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.Id)
	}
	model.MarkFileExtractsProcessed(ids)

	// 2. 每个仓库：取全量最新内容并提交
	for key := range touched {
		lock := repoLock(fmt.Sprintf("%d/%s", key.userId, key.project))
		lock.Lock()
		files, fileCount, err := model.LatestFilesForRepo(key.userId, key.project)
		if err != nil {
			common.SysLog(fmt.Sprintf("shadow: load files for %s/%s failed: %s", key.username, key.project, err.Error()))
			lock.Unlock()
			continue
		}
		repoDir := filepath.Join(ShadowRepoBaseDir(), sanitizeDirComponent(key.username), sanitizeDirComponent(key.project)+".git")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			common.SysLog("shadow: mkdir repo failed: " + err.Error())
			lock.Unlock()
			continue
		}
		msg := fmt.Sprintf("sync %s · %d files", time.Now().Format("2006-01-02 15:04"), fileCount)
		if err := commitToRepo(repoDir, key.username, msg, files); err != nil {
			common.SysLog(fmt.Sprintf("shadow: commit %s/%s failed: %s", key.username, key.project, err.Error()))
		}
		lock.Unlock()
	}
}

func sanitizeDirComponent(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	out := replacer.Replace(name)
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
