
    export type RemoteKeys = 'mfCopyButton/CopyButton';
    type PackageType<T> = T extends 'mfCopyButton/CopyButton' ? typeof import('mfCopyButton/CopyButton') :any;