package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	//"path/filepath"
	"strings"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"golang.org/x/net/context"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

func buildGridFsPath(parent_dir string, filename string) (string, error) {
	if parent_dir[0:1] != "/" {
		return "", errors.New("buildGridFsPath: invalid parent_dir specified, no / prefix")
	}
	if parent_dir == "/" {
		return filename, nil
	} else if len(parent_dir) > 1 {
		separator := "/"
		if len(filename) == 0 {
			separator = ""
		}
		return fmt.Sprintf("%s%s%s", parent_dir[1:], separator, filename), nil
	} else {
		return "", errors.New("buildGridFsPath: parent_dir must not be empty")
	}
}

// If there are files in a prefix, it is considered a directory.
func doFilesExistInGridFsPrefix(db *mgo.Database, parent_dir string, filename string) (bool, error) {
	gridfs_path, err := buildGridFsPath(parent_dir, filename)
	if err != nil {
		return false, err
	}
	regex := bson.RegEx{
		Pattern: fmt.Sprintf("^%s/.*", gridfs_path),
		Options: "",
	}
	log.Printf("doFilesExistInGridFsPrefix[%s, %s]: Regex = %s : %s\n", parent_dir, filename, regex.Pattern, regex.Options)
	query, err := filesInGridFsPrefixQuery(db, regex)
	if err != nil {
		return false, err
	}
	result, err := query.Count()
	if err != nil {
		return false, err
	}
	if result > 0 {
		return true, nil
	} else {
		return false, nil
	}
}

// Return files and directories in this explicit parent_dir (no subdirs counted).
// We need to be creative when it comes to directories, to build the list based on the child filenames.
func filesAndDirsInGridFsPrefix(db *mgo.Database, parent_dir string) ([]*mgo.GridFile, error) {
	gridfs_path, err := buildGridFsPath(parent_dir, "")
	if err != nil {
		return []*mgo.GridFile{}, err
	}
	// TODO: we need to filter out anything that is further down the directory hierarchy than the current directory.
	// would be more efficient to do it in the MongoDB query if possible? could be lots of childrens children
	fmt.Println(gridfs_path)
	regex := bson.RegEx{
		Pattern: `^.\*`,
		//fmt.Sprintf(`^%s.*`, gridfs_path),
		Options: "",
	}
	log.Printf("filesInGridFsPrefix[%s]: Regex = %s : %s\n", parent_dir, regex.Pattern, regex.Options)
	query, err := filesInGridFsPrefixQuery(db, regex)
	fmt.Printf("query: %+v\n", query)
	if err != nil {
		return []*mgo.GridFile{}, err
	}
	count, err := query.Count()
	if err != nil {
		return []*mgo.GridFile{}, err
	}
	fmt.Printf("Count: %d\n", count)
	iter := query.Iter()
	var results []*mgo.GridFile
	var result *mgo.GridFile
	for iter.Next(&result) {
		log.Printf("found file: %s\n", result.Name())
		results = append(results, result)
	}
	if err := iter.Close(); err != nil {
		return []*mgo.GridFile{}, err
	}
	return results, nil
}

func filesInGridFsPrefixQuery(db *mgo.Database, regex bson.RegEx) (*mgo.Query, error) {
	return db.GridFS("fs").Find(bson.M{"filename": bson.M{"$regex": regex}}), nil
}

////////////////////////////////////////////////////////

// GridFS implements the MongoDB GridFS filesystem
type GridFS struct{}

//Name string // the GridFS prefix
// TODO: verify if these are used?
//Dirent fuse.Dirent
//Fattr  fuse.Attr

func (g *GridFS) Root() (fs.Node, error) {
	log.Println("GridFS.Root() : Returning root node.")
	return &Dir{
		Inode: 1,
		Name:  "/",
		Mode:  0755,
		// Leave ModTime undefined for the root dir for now.
	}, nil
}

////////////////////////////////////////////////////////

type Dir struct {
	Inode     uint64
	Name      string
	ParentDir string
	Mode      os.FileMode // 0755
	ModTime   time.Time
	Uid       uint32
	Gid       uint32
}

var _ = fs.Node(&Dir{})

func (d *Dir) Attr(ctx context.Context, a *fuse.Attr) error {
	log.Printf("Dir[%s].Attr()\n", d.Name)
	a.Inode = d.Inode
	a.Mode = d.Mode
	a.Mtime = d.ModTime
	// Use the current user/group at all times, no references in MongoDB.
	a.Uid = uint32(os.Getuid())
	a.Gid = uint32(os.Getgid())
	log.Printf("Dir[%s].Attr(): Results set in fuse.Attr, returning nil (no error).\n", d.Name)
	return nil
}

// TODO: do we need to be defining DT_Dir based entries in the alternate ReadDirAll() below?
/*
func (d *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	log.Println("Dir.ReadDirAll():", d.name)

	db, s := getDb()
	defer s.Close()

	names, err := db.CollectionNames()
	if err != nil {
		log.Panic(err)
		return nil, fuse.EIO
	}

	ents := make([]fuse.Dirent, 0, len(names)+1) // one more for GridFS

	// Append GridFS prefix
	ents = append(ents, fuse.Dirent{Name: gridfsPrefix, Type: fuse.DT_Dir})

	// Append the rest of the collections
	for _, name := range names {
		if strings.HasSuffix(name, ".indexes") {
			continue
		}
		ents = append(ents, fuse.Dirent{Name: name, Type: fuse.DT_Dir})
	}
	return ents, nil
}
*/

////////////////////////////////////////////////////////

func (d *Dir) Lookup(ctx context.Context, filename string) (fs.Node, error) {
	log.Printf("Dir[%s].Lookup(%s)\n", d.Name, filename)

	// TODO: distinguish between root dir and a path? or is it already done for us?
	// TODO: change from object ID listings to filename listings in a directory hierarchy...

	//extIdx := strings.LastIndex(path, ".")
	//if extIdx > 0 {
	//	path = path[0:extIdx]
	//}
	//fmt.Printf("new path: %s\n", path)

	//if !bson.IsObjectIdHex(path) {
	//	log.Printf("Invalid ObjectId: %s\n", path)
	//	return nil, fuse.ENOENT
	//}

	db, s := getDb()
	defer s.Close()

	//id := bson.ObjectIdHex(path)
	//gf := File{Id: id}
	query := db.GridFS("fs").Find(bson.M{"filename": filename})
	file_exists, err := query.Count()
	if err == mgo.ErrNotFound || file_exists != 1 {
		// Could be a directory, check if any files exist with that dir prefix.
		is_dir, err := doFilesExistInGridFsPrefix(db, d.Name, filename)
		if err != nil {
			log.Printf("Dir[%s].Lookup(%s): Error from filesInGridFsPrefix(): %s\n", d.Name, filename, err.Error())
			return nil, fuse.EIO
		}
		if is_dir == true {
			log.Printf("Dir[%s].Lookup(%s): returning as a Dir{}.\n", d.Name, filename)
			return &Dir{
				Name: fmt.Sprintf("%s/%s", d.Name, filename),
				Mode: 0755,
				// TODOLATER: do we care about directory modtime? not easy to obtain?
				ParentDir: d.Name,
			}, nil
		} else {
			// Not a file in GridFS or a GridFS file prefix, doesn't exist.
			log.Printf("Dir[%s].Lookup(%s): Not a file in GridFS or a GridFS file prefix (aka directory), doesn't exist.\n", d.Name, filename)
			return nil, fuse.ENOENT
		}
	} else if err != nil {
		log.Printf("Dir[%s].Lookup(%s): Error checking if entry exists in GridFS: %s\n", d.Name, filename, err.Error())
		return nil, fuse.EIO
	}

	var result *mgo.GridFile
	err = query.One(&result)
	if err != nil {
		log.Printf("Dir[%s].Lookup(%s): Error retrieving from GridFS: %s\n", d.Name, filename, err.Error())
		return nil, fuse.EIO
	}
	log.Printf("%+v\n", result)
	log.Printf("Dir[%s].Lookup(%s): returning as a File{}.\n", d.Name, filename)
	return &File{
		MongoObjectId: result.Id().(bson.ObjectId),
		Name:          result.Name(),
		ParentDir:     d.Name,
	}, nil
}

func (d *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	log.Printf("Dir[%s].ReadDirAll()", d.Name)

	// TODO: change from object ID listings to filename listings in a directory hierarchy...

	db, s := getDb()
	defer s.Close()

	files, err := filesAndDirsInGridFsPrefix(db, d.Name)
	if err != nil {
		log.Printf("Could not list GridFS files: %s \n", err.Error())
		return nil, fuse.EIO
	}

	var ents []fuse.Dirent
	for _, v := range files {
		ents = append(ents, fuse.Dirent{
			Name: v.Name(),
			// TODO: some might be dirs, need to distinguish between them.
			Type: fuse.DT_File,
		})
	}

	//iter := gfs.Find().Iter()

	//var f *mgo.GridFile
	//for gfs.OpenNext(iter, &f) {
	//	name := f.Id().(bson.ObjectId).Hex() + filepath.Ext(f.Name())
	//	ents = append(ents, fuse.Dirent{Name: name, Type: fuse.DT_File})
	//}

	//if err := iter.Close(); err != nil {
	//	log.Printf("Could not list GridFS files: %s \n", err.Error())
	//	return nil, fuse.EIO
	//}

	return ents, nil
}

func (d *Dir) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	log.Printf("Dir[%s].Remove(): %s\n", d.Name, req.Name)

	id := req.Name
	extIdx := strings.LastIndex(id, ".")
	if extIdx > 0 {
		id = id[0:extIdx]
	}

	if !bson.IsObjectIdHex(id) {
		return fuse.ENOENT
	}

	db, s := getDb()
	defer s.Close()

	if err := db.GridFS("fs").RemoveId(bson.ObjectIdHex(id)); err != nil {
		log.Printf("Could not remove GridFS file '%s': %s \n", id, err.Error())
		return fuse.EIO
	}

	return nil
}

////////////////////////////////////////////////////////

// File implements both Node and Handle for a document from a collection.
type File struct {
	MongoObjectId bson.ObjectId `bson:"_id"`
	Name          string
	ParentDir     string

	Dirent fuse.Dirent
	Fattr  fuse.Attr
}

func (f *File) Attr(ctx context.Context, a *fuse.Attr) error {
	log.Printf("File.Attr() for: %+v", f)

	db, s := getDb()
	defer s.Close()

	file, err := db.GridFS("fs").OpenId(f.MongoObjectId)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	a.Mode = 0400
	a.Uid = uint32(os.Getuid())
	a.Gid = uint32(os.Getgid())
	a.Size = uint64(file.Size())
	a.Ctime = file.UploadDate()
	a.Atime = time.Now()
	a.Mtime = file.UploadDate()
	return nil
}

func (f *File) Lookup(ctx context.Context, path string) (fs.Node, error) {
	log.Printf("File.Lookup(): %s\n", path)

	return nil, fuse.ENOENT
}

// TODO: do chunked reads instead using Read(), far nicer than ReadAll()
func (f *File) ReadAll(ctx context.Context) ([]byte, error) {
	log.Printf("File.ReadAll(): %s\n", f.MongoObjectId)

	db, s := getDb()
	defer s.Close()

	file, err := db.GridFS("fs").OpenId(f.MongoObjectId)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// TODO: return a pointer to the buffer? memory usage?
	buf, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return buf, nil
}
