package pbm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
)

type Node struct {
	rs        string
	me        string
	ctx       context.Context
	cn        *mongo.Client
	curi      string
	dumpConns int
}

// ReplsetRole is a replicaset role in sharded cluster
type ReplsetRole string

const (
	RoleUnknown   ReplsetRole = "unknown"
	RoleShard     ReplsetRole = "shard"
	RoleConfigSrv ReplsetRole = "configsrv"

	// TmpUsersCollection and TmpRoles are tmp collections used to avoid
	// user related issues while resoring on new cluster and preserving UUID
	// See https://jira.percona.com/browse/PBM-425, https://jira.percona.com/browse/PBM-636
	TmpUsersCollection = `pbmRUsers`
	TmpRolesCollection = `pbmRRoles`
)

func NewNode(ctx context.Context, curi string, dumpConns int) (*Node, error) {
	n := &Node{
		ctx:       ctx,
		curi:      curi,
		dumpConns: dumpConns,
	}
	err := n.Connect()
	if err != nil {
		return nil, errors.Wrap(err, "connect")
	}

	nodeInfo, err := n.GetInfo()
	if err != nil {
		return nil, errors.Wrap(err, "get node info")
	}
	n.rs, n.me = nodeInfo.SetName, nodeInfo.Me

	return n, nil
}

// ID returns node ID
func (n *Node) ID() string {
	return fmt.Sprintf("%s/%s", n.rs, n.me)
}

// RS return replicaset name node belongs to
func (n *Node) RS() string {
	return n.rs
}

// Name returns node name
func (n *Node) Name() string {
	return n.me
}

func (n *Node) Connect() error {
	conn, err := n.connect(true)
	if err != nil {
		return err
	}

	if n.cn != nil {
		err = n.cn.Disconnect(n.ctx)
		if err != nil {
			return errors.Wrap(err, "close existing connection")
		}
	}

	n.cn = conn
	return nil
}

func (n *Node) connect(direct bool) (*mongo.Client, error) {
	conn, err := mongo.NewClient(options.Client().ApplyURI(n.curi).SetAppName("pbm-agent-exec").SetDirect(direct))
	if err != nil {
		return nil, errors.Wrap(err, "create mongo client")
	}
	err = conn.Connect(n.ctx)
	if err != nil {
		return nil, errors.Wrap(err, "connect")
	}

	err = conn.Ping(n.ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "ping")
	}

	return conn, nil
}

func (n *Node) GetInfo() (*NodeInfo, error) {
	i := &NodeInfo{}
	err := n.cn.Database(DB).RunCommand(n.ctx, bson.D{{"isMaster", 1}}).Decode(i)
	if err != nil {
		return nil, errors.Wrap(err, "get NodeInfo")
	}
	opts, err := n.GetOpts()
	if err != nil {
		return nil, errors.Wrap(err, "get mongod options")
	}
	if opts != nil {
		i.opts = *opts
	}
	return i, nil
}

// SizeDBs returns the total size in bytes of all databases' files on disk on replicaset
func (n *Node) SizeDBs() (int, error) {
	i := &struct {
		TotalSize int `bson:"totalSize"`
	}{}
	err := n.cn.Database(DB).RunCommand(n.ctx, bson.D{{"listDatabases", 1}}).Decode(i)
	if err != nil {
		return 0, errors.Wrap(err, "run mongo command listDatabases")
	}
	return i.TotalSize, nil
}

// IsSharded return true if node is part of the sharded cluster (in shard or configsrv replset).
func (n *Node) IsSharded() (bool, error) {
	i, err := n.GetInfo()
	if err != nil {
		return false, err
	}

	return i.IsSharded(), nil
}

type MongoVersion struct {
	VersionString string `bson:"version"`
	Version       []int  `bson:"versionArray"`
}

func (n *Node) GetMongoVersion() (*MongoVersion, error) {
	ver := new(MongoVersion)
	err := n.cn.Database(DB).RunCommand(n.ctx, bson.D{{"buildInfo", 1}}).Decode(ver)
	return ver, err
}

func (n *Node) GetReplsetStatus() (*ReplsetStatus, error) {
	return GetReplsetStatus(n.ctx, n.cn)
}

func (n *Node) Status() (*NodeStatus, error) {
	s, err := n.GetReplsetStatus()
	if err != nil {
		return nil, errors.Wrap(err, "get replset status")
	}

	name := n.Name()

	for _, m := range s.Members {
		if m.Name == name {
			return &m, nil
		}
	}

	return nil, ErrNotFound
}

// ReplicationLag returns node replication lag in seconds
func (n *Node) ReplicationLag() (int, error) {
	s, err := n.GetReplsetStatus()
	if err != nil {
		return -1, errors.Wrap(err, "get replset status")
	}

	name := n.Name()

	var primaryOptime, nodeOptime int
	for _, m := range s.Members {
		if m.Name == name {
			nodeOptime = int(m.Optime.TS.T)
		}
		if m.StateStr == "PRIMARY" {
			primaryOptime = int(m.Optime.TS.T)
		}
	}

	return primaryOptime - nodeOptime, nil
}

func (n *Node) ConnURI() string {
	return n.curi
}

func (n *Node) DumpConns() int {
	return n.dumpConns
}

func (n *Node) Session() *mongo.Client {
	return n.cn
}

func (n *Node) CurrentUser() (*AuthInfo, error) {
	c := &ConnectionStatus{}
	err := n.cn.Database(DB).RunCommand(n.ctx, bson.D{{"connectionStatus", 1}}).Decode(c)
	if err != nil {
		return nil, errors.Wrap(err, "run mongo command connectionStatus")
	}

	return &c.AuthInfo, nil
}

func (n *Node) DropTMPcoll() error {
	cn, err := n.connect(false)
	if err != nil {
		return errors.Wrap(err, "connect to primary")
	}
	defer cn.Disconnect(n.ctx)

	err = DropTMPcoll(n.ctx, cn)
	if err != nil {
		return err
	}

	return nil
}

func DropTMPcoll(ctx context.Context, cn *mongo.Client) error {
	err := cn.Database(DB).Collection(TmpRolesCollection).Drop(ctx)
	if err != nil {
		return errors.Wrapf(err, "drop collection %s", TmpRolesCollection)
	}

	err = cn.Database(DB).Collection(TmpUsersCollection).Drop(ctx)
	if err != nil {
		return errors.Wrapf(err, "drop collection %s", TmpUsersCollection)
	}

	return nil
}

func (n *Node) WaitForWrite(ts primitive.Timestamp) (err error) {
	var lw primitive.Timestamp
	for i := 0; i < 21; i++ {
		lw, err = LastWrite(n.cn, false)
		if err == nil && primitive.CompareTimestamp(lw, ts) >= 0 {
			return nil
		}
		time.Sleep(time.Second * 1)
	}

	if err != nil {
		return err
	}

	return errors.New("run out of time")
}

func LastWrite(cn *mongo.Client, majority bool) (primitive.Timestamp, error) {
	inf := &NodeInfo{}
	err := cn.Database("admin").RunCommand(context.Background(), bson.D{{"isMaster", 1}}).Decode(inf)
	if err != nil {
		return primitive.Timestamp{}, errors.Wrap(err, "get NodeInfo data")
	}
	lw := inf.LastWrite.MajorityOpTime.TS
	if !majority {
		lw = inf.LastWrite.OpTime.TS
	}
	if lw.T == 0 {
		return primitive.Timestamp{}, errors.New("last write timestamp is nil")
	}
	return lw, nil
}

// OplogStartTime returns either the oldest active transaction timestamp or the
// current oplog time if there are no active transactions.
// taken from https://github.com/mongodb/mongo-tools/blob/1b496c4a8ff7415abc07b9621166d8e1fac00c91/mongodump/oplog_dump.go#L68
func (n *Node) OplogStartTime() (primitive.Timestamp, error) {
	coll := n.cn.Database("config").Collection("transactions", options.Collection().SetReadConcern(readconcern.Local()))
	filter := bson.D{{"state", bson.D{{"$in", bson.A{"prepared", "inProgress"}}}}}
	opts := options.FindOne().SetSort(bson.D{{"startOpTime", 1}})

	var result bson.Raw
	res := coll.FindOne(context.Background(), filter, opts)
	err := res.Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return LastWrite(n.cn, true)
		}
		return primitive.Timestamp{}, fmt.Errorf("config.transactions.findOne error: %v", err)
	}

	rawTS, err := result.LookupErr("startOpTime", "ts")
	if err != nil {
		return primitive.Timestamp{}, fmt.Errorf("config.transactions row had no startOpTime.ts field")
	}

	t, i, ok := rawTS.TimestampOK()
	if !ok {
		return primitive.Timestamp{}, fmt.Errorf("config.transactions startOpTime.ts was not a BSON timestamp")
	}

	return primitive.Timestamp{T: t, I: i}, nil
}

func (n *Node) CopyUsersNRolles() (lastWrite primitive.Timestamp, err error) {
	cn, err := n.connect(false)
	if err != nil {
		return lastWrite, errors.Wrap(err, "connect to primary")
	}
	defer cn.Disconnect(n.ctx)

	err = DropTMPcoll(n.ctx, cn)
	if err != nil {
		return lastWrite, errors.Wrap(err, "drop tmp collections before copy")

	}

	_, err = CopyColl(n.ctx,
		cn.Database("admin").Collection("system.roles"),
		cn.Database(DB).Collection(TmpRolesCollection),
		bson.M{},
	)
	if err != nil {
		return lastWrite, errors.Wrap(err, "copy admin.system.roles")
	}
	_, err = CopyColl(n.ctx,
		cn.Database("admin").Collection("system.users"),
		cn.Database(DB).Collection(TmpUsersCollection),
		bson.M{},
	)
	if err != nil {
		return lastWrite, errors.Wrap(err, "copy admin.system.users")
	}

	return LastWrite(cn, false)
}

func (n *Node) GetOpts() (*MongodOpts, error) {
	opts := struct {
		Parsed MongodOpts `bson:"parsed" json:"parsed"`
	}{}
	err := n.cn.Database("admin").RunCommand(n.ctx, bson.D{{"getCmdLineOpts", 1}}).Decode(&opts)
	if err != nil {
		return nil, errors.Wrap(err, "run mongo command")
	}
	return &opts.Parsed, nil
}

func (n *Node) GetRSconf() (*RSConfig, error) {
	rsc := struct {
		Conf RSConfig `bson:"config" json:"config"`
	}{}
	err := n.cn.Database("admin").RunCommand(n.ctx, bson.D{{"replSetGetConfig", 1}}).Decode(&rsc)
	if err != nil {
		return nil, errors.Wrap(err, "run mongo command")
	}
	return &rsc.Conf, nil
}

func (n *Node) Shutdown() error {
	err := n.cn.Database("admin").RunCommand(n.ctx, bson.D{{"shutdown", 1}}).Err()
	if err == nil || strings.Contains(err.Error(), "socket was unexpectedly closed") {
		return nil
	}
	return err
}
