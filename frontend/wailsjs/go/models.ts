export namespace config {
	
	export class AppConfig {
	    version: string;
	    backends: plugins.BackendConfig[];
	    cacheEnabled: boolean;
	    cacheDir: string;
	    cacheSizeMaxMB: number;
	    startMinimized: boolean;
	    autoStart: boolean;
	    ghostDriveRoot?: string;
	    mountPoint?: string;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.backends = this.convertValues(source["backends"], plugins.BackendConfig);
	        this.cacheEnabled = source["cacheEnabled"];
	        this.cacheDir = source["cacheDir"];
	        this.cacheSizeMaxMB = source["cacheSizeMaxMB"];
	        this.startMinimized = source["startMinimized"];
	        this.autoStart = source["autoStart"];
	        this.ghostDriveRoot = source["ghostDriveRoot"];
	        this.mountPoint = source["mountPoint"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace loader {
	
	export class PluginInfo {
	    name: string;
	    version: string;
	    path: string;
	    status: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.path = source["path"];
	        this.status = source["status"];
	        this.error = source["error"];
	    }
	}

}

export namespace placeholder {
	
	export class DriveStatus {
	    Mounted: boolean;
	    MountPoint: string;
	    BackendPaths: Record<string, string>;
	    LastError: string;
	
	    static createFrom(source: any = {}) {
	        return new DriveStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Mounted = source["Mounted"];
	        this.MountPoint = source["MountPoint"];
	        this.BackendPaths = source["BackendPaths"];
	        this.LastError = source["LastError"];
	    }
	}

}

export namespace plugins {
	
	export class BackendConfig {
	    id: string;
	    name: string;
	    type: string;
	    enabled: boolean;
	    autoSync: boolean;
	    params: Record<string, string>;
	    syncDir: string;
	    remotePath: string;
	    localPath: string;
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new BackendConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.enabled = source["enabled"];
	        this.autoSync = source["autoSync"];
	        this.params = source["params"];
	        this.syncDir = source["syncDir"];
	        this.remotePath = source["remotePath"];
	        this.localPath = source["localPath"];
	        this.warning = source["warning"];
	    }
	}
	export class FileInfo {
	    name: string;
	    path: string;
	    size: number;
	    isDir: boolean;
	    // Go type: time
	    modTime: any;
	    etag: string;
	    isPlaceholder: boolean;
	    isCached: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.size = source["size"];
	        this.isDir = source["isDir"];
	        this.modTime = this.convertValues(source["modTime"], null);
	        this.etag = source["etag"];
	        this.isPlaceholder = source["isPlaceholder"];
	        this.isCached = source["isCached"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ParamSpec {
	    key: string;
	    label: string;
	    type: string;
	    required: boolean;
	    default: string;
	    placeholder: string;
	    options?: string[];
	    helpText?: string;
	
	    static createFrom(source: any = {}) {
	        return new ParamSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.default = source["default"];
	        this.placeholder = source["placeholder"];
	        this.options = source["options"];
	        this.helpText = source["helpText"];
	    }
	}
	export class PluginDescriptor {
	    type: string;
	    displayName: string;
	    description: string;
	    params: ParamSpec[];
	
	    static createFrom(source: any = {}) {
	        return new PluginDescriptor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.displayName = source["displayName"];
	        this.description = source["description"];
	        this.params = this.convertValues(source["params"], ParamSpec);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace types {
	
	export class BackendStatus {
	    backendId: string;
	    connected: boolean;
	    error?: string;
	    freeSpace: number;
	    totalSpace: number;
	
	    static createFrom(source: any = {}) {
	        return new BackendStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.backendId = source["backendId"];
	        this.connected = source["connected"];
	        this.error = source["error"];
	        this.freeSpace = source["freeSpace"];
	        this.totalSpace = source["totalSpace"];
	    }
	}
	export class BackendSyncError {
	    backendId: string;
	    path: string;
	    message: string;
	    // Go type: time
	    time: any;
	
	    static createFrom(source: any = {}) {
	        return new BackendSyncError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.backendId = source["backendId"];
	        this.path = source["path"];
	        this.message = source["message"];
	        this.time = this.convertValues(source["time"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BackendSyncState {
	    backendId: string;
	    backendName: string;
	    status: string;
	    progress: number;
	    currentFile: string;
	    pending: number;
	    errors: BackendSyncError[];
	    // Go type: time
	    lastSync: any;
	
	    static createFrom(source: any = {}) {
	        return new BackendSyncState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.backendId = source["backendId"];
	        this.backendName = source["backendName"];
	        this.status = source["status"];
	        this.progress = source["progress"];
	        this.currentFile = source["currentFile"];
	        this.pending = source["pending"];
	        this.errors = this.convertValues(source["errors"], BackendSyncError);
	        this.lastSync = this.convertValues(source["lastSync"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CacheStats {
	    sizeMB: number;
	    fileCount: number;
	    maxSizeMB: number;
	
	    static createFrom(source: any = {}) {
	        return new CacheStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sizeMB = source["sizeMB"];
	        this.fileCount = source["fileCount"];
	        this.maxSizeMB = source["maxSizeMB"];
	    }
	}
	export class ProgressEvent {
	    path: string;
	    direction: string;
	    bytesDone: number;
	    bytesTotal: number;
	    percent: number;
	
	    static createFrom(source: any = {}) {
	        return new ProgressEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.direction = source["direction"];
	        this.bytesDone = source["bytesDone"];
	        this.bytesTotal = source["bytesTotal"];
	        this.percent = source["percent"];
	    }
	}
	export class SyncErrorInfo {
	    path: string;
	    message: string;
	    // Go type: time
	    time: any;
	
	    static createFrom(source: any = {}) {
	        return new SyncErrorInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.message = source["message"];
	        this.time = this.convertValues(source["time"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SyncState {
	    status: string;
	    progress: number;
	    currentFile: string;
	    pending: number;
	    errors: SyncErrorInfo[];
	    // Go type: time
	    lastSync: any;
	    backends: BackendSyncState[];
	    activeTransfers: ProgressEvent[];
	
	    static createFrom(source: any = {}) {
	        return new SyncState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.progress = source["progress"];
	        this.currentFile = source["currentFile"];
	        this.pending = source["pending"];
	        this.errors = this.convertValues(source["errors"], SyncErrorInfo);
	        this.lastSync = this.convertValues(source["lastSync"], null);
	        this.backends = this.convertValues(source["backends"], BackendSyncState);
	        this.activeTransfers = this.convertValues(source["activeTransfers"], ProgressEvent);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

