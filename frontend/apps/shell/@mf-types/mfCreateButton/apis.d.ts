
    export type RemoteKeys = 'mfCreateButton/CreateButton';
    type PackageType<T> = T extends 'mfCreateButton/CreateButton' ? typeof import('mfCreateButton/CreateButton') :any;