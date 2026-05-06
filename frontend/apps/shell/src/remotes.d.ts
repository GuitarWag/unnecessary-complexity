declare module 'mfUrlInput/UrlInput' {
  export const UrlInput: React.ComponentType;
}

declare module 'mfCreateButton/CreateButton' {
  export const CreateButton: React.ComponentType<{
    onClick?: () => void;
    children?: React.ReactNode;
    disabled?: boolean;
  }>;
}

declare module 'mfCopyButton/CopyButton' {
  export const CopyButton: React.ComponentType<{ text: string }>;
}

declare module 'mfUrlList/UrlList' {
  export const UrlList: React.ComponentType<{
    codes: readonly string[];
    baseUrl?: string;
    onSelect?: (code: string) => void;
  }>;
}

declare module 'mfAnalyticsChart/AnalyticsChart' {
  export const AnalyticsChart: React.ComponentType<{ code: string }>;
}
