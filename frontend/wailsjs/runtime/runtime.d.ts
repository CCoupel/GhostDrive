export declare function EventsOn(eventName: string, callback: (...data: unknown[]) => void): () => void;
export declare function EventsOnce(eventName: string, callback: (...data: unknown[]) => void): void;
export declare function EventsOff(...eventNames: string[]): void;
export declare function EventsEmit(eventName: string, ...data: unknown[]): void;
export declare function WindowMinimise(): void;
export declare function WindowHide(): void;
export declare function WindowShow(): void;
export declare function Quit(): void;
