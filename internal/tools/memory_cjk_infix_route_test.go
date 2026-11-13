package tools

// Shared harness for the CJK route measurements (epic 01a0e4b4): the 12-row
// corpus, the per-route ID readers, and the public-entry-point reader the
// mechanism tests in memory_cjk_infix_route_mechanism_test.go drive.
//
// The corpus and the readers are mechanism-independent; the ASSERTIONS that
// measured the old 2-char-gram tokenizer were rewritten when the segmenter
// changed (see that file's header for why).

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// cjkInfixCorpus is the 12-row probe corpus. Rows 1/2 and 5/6 deliberately
// share the CJK runs 网络测试环境 / 测试 so the infix shapes have a hit set with
// more than one member.
var cjkInfixCorpus = []struct{ topic, question, answer string }{
	{"网络测试环境部署完成", "网络测试环境部署完成需要哪些前置条件？",
		"前置条件：一台可用的宿主、镜像仓库凭据、以及规划好的网段；部署脚本分三步执行，先校验再落盘。"},
	{"VPN 网络测试环境", "VPN 网络测试环境如何与办公网隔离？",
		"隔离做法：为测试环境单独划一个 VLAN，路由只在跳板机上暴露，办公网默认路由不指向该网段。"},
	{"deepseek开支优化", "deepseek 调用开支怎么降低？",
		"降低开支的三条：把长 prompt 缓存命中率提上去、把批量任务挪到夜间便宜时段、单次请求的上下文上限收紧。"},
	{"数据库备份策略", "数据库备份策略应该怎么定？",
		"策略：全量每周一次、增量每小时一次，备份文件异地存放，并每季度做一次恢复演练验证可还原。"},
	{"openstack nova scheduling", "openstack nova scheduling tuning 有哪些要点？",
		"要点：把 nova 的调度器权重按机房改、打开 aggregate 亲和、限制单宿主的并发构建数，避免雪崩。"},
	{"测试用例集管理", "测试用例集管理怎么避免重复？",
		"管理做法：用例按功能域分文件、命名带前缀、提交前跑一遍去重扫描，重复用例直接合并删除。"},
	{"缓存失效处理", "缓存失效处理有哪些常见坑？",
		"常见坑：雪崩、穿透、击穿三个；对策分别是随机过期、空值占位、单飞锁，别只做一个。"},
	{"网络延迟排查", "网络延迟排查的步骤是什么？",
		"排查步骤：先分层（链路、DNS、TLS、应用）、再分段计时，最后对比正常路径的基线，定位到跳。"},
	{"golang内存泄漏排查", "golang 内存泄漏排查怎么做？",
		"排查做法：取 profile 的两个时间点做 diff，看保留堆的增长源，再顺着引用链找到没释放的 goroutine。"},
	{"夜间批量任务调度", "夜间批量任务调度怎么排？",
		"排法：按依赖拓扑分层，宽任务并发、深任务串行，并把重活压到夜里便宜时段，白天只跑增量。"},
	{"redis集群扩容", "redis 集群扩容怎么平滑做？",
		"平滑做法：加节点后逐个迁移 slot，迁移期间客户端开 MOVED 重定向，迁完再摘掉旧节点。"},
	{"单元测试覆盖率报告", "单元测试覆盖率报告怎么生成？",
		"生成方式：go test -coverprofile 出文件，再 go tool cover -html 出页面，CI 里存一份趋势。"},
}

// seedCJKInfixCorpus stores the corpus through the production writer and
// returns topic → ledger id.
func seedCJKInfixCorpus(t *testing.T) map[string]int {
	t.Helper()
	for i, r := range cjkInfixCorpus {
		if _, err := MemoryStore(r.topic, r.question, r.answer, "probe/epic-01a0e4b4",
			[]string{"probe", fmt.Sprintf("row%d", i+1)}); err != nil {
			t.Fatalf("store row %d (%s): %v", i+1, r.topic, err)
		}
	}
	ctx := context.Background()
	db := openRawDB(t, memoryDBPath())
	rows, err := db.QueryContext(ctx, `SELECT id, topic FROM distilled_qa ORDER BY id`)
	if err != nil {
		t.Fatalf("read back corpus: %v", err)
	}
	defer rows.Close()
	ids := map[string]int{}
	for rows.Next() {
		var id int
		var topic string
		if err := rows.Scan(&id, &topic); err != nil {
			t.Fatal(err)
		}
		ids[topic] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ids) != len(cjkInfixCorpus) {
		t.Fatalf("corpus seeded %d rows, want %d — a row was deduped or rejected", len(ids), len(cjkInfixCorpus))
	}
	return ids
}

// ftsRouteIDs runs the SAME MATCH expression MemorySearchHits sends for one FTS
// route: route 1 (distilled_qa_fts + escapeQuery) or route 2 (distilled_qa_tri +
// the OR-joined cjkRunsGE3).
func ftsRouteIDs(t *testing.T, query, table string) []int {
	t.Helper()
	term, ok := escapeQuery(query)
	if !ok {
		return nil
	}
	if table == memoryTriIndexTable {
		segs := cjkRunsGE3(query)
		if len(segs) == 0 {
			return nil
		}
		parts := make([]string, 0, len(segs))
		for _, s := range segs {
			parts = append(parts, `"`+s+`"`)
		}
		term = strings.Join(parts, " OR ")
	}
	return scanIDs(t, query, `SELECT d.id FROM `+table+` x JOIN distilled_qa d ON d.id = x.rowid
		WHERE `+table+` MATCH ? ORDER BY d.id`, term)
}

// likeRouteIDs runs route 3 as MemorySearchHits sends it: the query's
// SEGMENTED words scanned as substrings over the LIKE columns.
func likeRouteIDs(t *testing.T, query string) []int {
	t.Helper()
	terms := cjkQuerySegments(query)
	if len(terms) == 0 {
		return nil
	}
	args := appendCJKLikes(terms, len(terms))
	return scanIDs(t, query, `SELECT d.id FROM distilled_qa d WHERE `+
		strings.Join(likeCJKClauses(len(terms)), " OR ")+` ORDER BY d.id`, args...)
}

func scanIDs(t *testing.T, query, sqlText string, args ...interface{}) []int {
	t.Helper()
	db := openRawDB(t, memoryDBPath())
	rows, err := db.QueryContext(context.Background(), sqlText, args...)
	if err != nil {
		t.Fatalf("route query %q: %v", query, err)
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Ints(ids)
	return ids
}

// publicHitIDs is what a caller actually sees.
func publicHitIDs(t *testing.T, query string) []int {
	t.Helper()
	hits, err := MemorySearchHits(query, 20, "")
	if err != nil {
		t.Fatalf("MemorySearchHits(%q): %v", query, err)
	}
	ids := make([]int, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ID)
	}
	sort.Ints(ids)
	return ids
}

func sameIDSet(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsInt(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
