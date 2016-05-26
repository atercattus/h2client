package h2client

import (
	"github.com/pkg/errors"
	"sync"
	"sync/atomic"
	"time"
)

type (
	connectionPoolItem struct {
		sync.RWMutex
		conns []*Connection
	}

	ConnectionPool struct {
		pool            map[string]*connectionPoolItem
		poolMu          sync.RWMutex
		maxConnsPerHost int
	}
)

func NewConnectionPool(maxConnsPerHost int) *ConnectionPool {
	pool := ConnectionPool{}
	pool.pool = make(map[string]*connectionPoolItem)

	if maxConnsPerHost <= 0 {
		maxConnsPerHost = 10
	}
	pool.maxConnsPerHost = maxConnsPerHost

	go pool.closedConnRemover()

	return &pool
}

func (p *ConnectionPool) Do(req *request) (*response, error) {
	return p.do(req, nil)
}

// hostPool должен быть под write-lock
func (p *ConnectionPool) removeClosedConnections(hostPool *connectionPoolItem) {
	cnt := len(hostPool.conns)
	for i := 0; i < cnt; i++ {
		if conn := hostPool.conns[i]; conn.IsConsideredClosed() || conn.HasGoAwayFrames() {
			if i < cnt-1 {
				hostPool.conns[i] = hostPool.conns[cnt-1]
			}
			i--
			cnt--
			hostPool.conns = hostPool.conns[0:cnt]
		}
	}
}

func (p *ConnectionPool) closedConnRemover() {
	ticker := time.NewTicker(10 * time.Second)
	for {
		<-ticker.C

		p.poolMu.RLock()
		for _, hostPool := range p.pool {
			hostPool.Lock()
			p.removeClosedConnections(hostPool)
			hostPool.Unlock()
		}
		p.poolMu.RUnlock()
	}
}

func (p *ConnectionPool) do(req *request, failedConns []*Connection) (*response, error) {
	for try := 1; try < 10; try++ {
		conn, err := p.getConn(req, failedConns)
		if err != nil {
			return nil, errors.Wrap(err, `There are no available connections`)
		}

		resp, err := conn.reqWithLockedStream(req)
		if err != nil {
			failedConns = append(failedConns, conn)
			p.retConn(req, conn)
			continue
		}

		p.retConn(req, conn)

		return resp, err
	}
	return nil, errors.Wrap(ErrPoolCapacityLimit, `There are no available working connections`)
}

func (p *ConnectionPool) selectConnInPool(hostPool *connectionPoolItem, failedConns []*Connection) *Connection {
	connsLen := int64(len(hostPool.conns))
	if connsLen == 0 {
		return nil
	}

	fromIdx := time.Now().UnixNano() % connsLen
	curIdx := fromIdx
	for {
		conn := hostPool.conns[curIdx]
		if conn.IsConsideredClosed() || conn.HasGoAwayFrames() {
			// закрыто, но еще не удалено из пула (удаление асинхронное)
			// do nothing
		} else if p.isConnInList(conn, failedConns) {
			// соединение, с которым у нас уже не получилось выполнить запрос
			// do nothing
		} else if conn.LockStream() {
			return conn
		} else {
			fcwef := atomic.LoadInt64(&conn.flowControlWindowEmptyFrom)
			if diff := nowCached.Unix() - fcwef; fcwef > 0 && diff >= 10 { // ToDo: 10 сек вынести в конфиг
				conn.WantClose()
				failedConns = append(failedConns, conn)
			}
		}

		if curIdx = (curIdx + 1) % connsLen; curIdx == fromIdx {
			break
		}
	}

	return nil
}

func (p *ConnectionPool) isConnInList(conn *Connection, lst []*Connection) bool {
	for _, c := range lst {
		if c == conn {
			return true
		}
	}
	return false
}

func (p *ConnectionPool) getConn(req *request, failedConns []*Connection) (conn *Connection, err error) {
	for try := 1; try <= 10; try++ {
		conn, err = p.getConnInternal(req, failedConns)
		if conn != nil {
			return conn, nil
		} else if err != nil {
			return nil, err
		}
	}
	return nil, errors.Wrap(ErrPoolCapacityLimit, `Cannot get new connetion`)
}

func (p *ConnectionPool) getConnInternal(req *request, failedConns []*Connection) (conn *Connection, err error) {
	poolKey := req.getCacheKey()

	p.poolMu.RLock()
	hostPool, ok := p.pool[poolKey]
	if !ok {
		p.poolMu.RUnlock()
		p.poolMu.Lock()
		if hostPool, ok = p.pool[poolKey]; !ok {
			hostPool = &connectionPoolItem{}
			p.pool[poolKey] = hostPool
		}
		p.poolMu.Unlock()
	} else {
		p.poolMu.RUnlock()
	}

	hostPool.RLock()
	conn = p.selectConnInPool(hostPool, failedConns)
	hostPool.RUnlock()
	if conn != nil {
		return conn, nil
	}

	// Все имеющиеся в пуле соединения нагружены по полной.
	// Нужно выделить еще один коннект (если не превысили лимит).

	hostPool.Lock()

	if conn := p.selectConnInPool(hostPool, failedConns); conn != nil {
		// таки нашли подходящий коннект
		hostPool.Unlock()
		return conn, nil
	}

	// ToDo: считать количество рабочих соединений без учета закрытых
	if len(hostPool.conns) >= p.maxConnsPerHost {
		hostPool.Unlock()
		return nil, errors.Wrap(ErrPoolCapacityLimit, `Limit check`)
	}

	if conn, err = NewConnection(req); err != nil {
		hostPool.Unlock()
		return nil, errors.Wrap(err, `Cannot establish new connection`)
	}

	hostPool.conns = append(hostPool.conns, conn)

	hostPool.Unlock()

	<-conn.GetConnLocker() // ждем завершения http/2 хендшейка. стоит ли?

	return nil, nil
}

func (p *ConnectionPool) retConn(req *request, conn *Connection) {
	conn.UnlockStream()
	// ToDo: уменьшать размер пула, если есть пустые соединения (заодно ввести MaxIdleConnsPerHost и выкинуть(?) maxConnsPerHost)
}
