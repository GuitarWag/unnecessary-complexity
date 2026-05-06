import type { GenFile, GenMessage, GenService } from "@bufbuild/protobuf/codegenv1";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type { Message } from "@bufbuild/protobuf";
/**
 * Describes the file shortener/v1/shortener.proto.
 */
export declare const file_shortener_v1_shortener: GenFile;
/**
 * @generated from message shortener.v1.ShortenRequest
 */
export type ShortenRequest = Message<"shortener.v1.ShortenRequest"> & {
    /**
     * @generated from field: string long_url = 1;
     */
    longUrl: string;
    /**
     * Optional opaque idempotency key. Two requests from the same owner_id with the same
     * non-empty key are guaranteed to return the same code. Empty key disables dedup.
     *
     * @generated from field: string idempotency_key = 2;
     */
    idempotencyKey: string;
    /**
     * Optional owner identifier. Scopes idempotency_key uniqueness so users can't collide.
     * Empty owner_id is treated as a single shared namespace.
     *
     * @generated from field: string owner_id = 3;
     */
    ownerId: string;
};
/**
 * Describes the message shortener.v1.ShortenRequest.
 * Use `create(ShortenRequestSchema)` to create a new message.
 */
export declare const ShortenRequestSchema: GenMessage<ShortenRequest>;
/**
 * @generated from message shortener.v1.ShortenResponse
 */
export type ShortenResponse = Message<"shortener.v1.ShortenResponse"> & {
    /**
     * @generated from field: shortener.v1.ShortURL url = 1;
     */
    url?: ShortURL;
    /**
     * false if an existing code was returned.
     *
     * @generated from field: bool created = 2;
     */
    created: boolean;
};
/**
 * Describes the message shortener.v1.ShortenResponse.
 * Use `create(ShortenResponseSchema)` to create a new message.
 */
export declare const ShortenResponseSchema: GenMessage<ShortenResponse>;
/**
 * @generated from message shortener.v1.GetRequest
 */
export type GetRequest = Message<"shortener.v1.GetRequest"> & {
    /**
     * @generated from field: string code = 1;
     */
    code: string;
};
/**
 * Describes the message shortener.v1.GetRequest.
 * Use `create(GetRequestSchema)` to create a new message.
 */
export declare const GetRequestSchema: GenMessage<GetRequest>;
/**
 * @generated from message shortener.v1.GetResponse
 */
export type GetResponse = Message<"shortener.v1.GetResponse"> & {
    /**
     * @generated from field: shortener.v1.ShortURL url = 1;
     */
    url?: ShortURL;
};
/**
 * Describes the message shortener.v1.GetResponse.
 * Use `create(GetResponseSchema)` to create a new message.
 */
export declare const GetResponseSchema: GenMessage<GetResponse>;
/**
 * @generated from message shortener.v1.ShortURL
 */
export type ShortURL = Message<"shortener.v1.ShortURL"> & {
    /**
     * @generated from field: string code = 1;
     */
    code: string;
    /**
     * @generated from field: string long_url = 2;
     */
    longUrl: string;
    /**
     * @generated from field: google.protobuf.Timestamp created_at = 3;
     */
    createdAt?: Timestamp;
};
/**
 * Describes the message shortener.v1.ShortURL.
 * Use `create(ShortURLSchema)` to create a new message.
 */
export declare const ShortURLSchema: GenMessage<ShortURL>;
/**
 * ShortenerService is the write-side API for short URLs.
 *
 * @generated from service shortener.v1.ShortenerService
 */
export declare const ShortenerService: GenService<{
    /**
     * Shorten creates a short code for a long URL. Idempotent on long_url:
     * submitting the same long_url twice returns the existing code.
     *
     * @generated from rpc shortener.v1.ShortenerService.Shorten
     */
    shorten: {
        methodKind: "unary";
        input: typeof ShortenRequestSchema;
        output: typeof ShortenResponseSchema;
    };
    /**
     * Get returns the mapping for a code.
     *
     * @generated from rpc shortener.v1.ShortenerService.Get
     */
    get: {
        methodKind: "unary";
        input: typeof GetRequestSchema;
        output: typeof GetResponseSchema;
    };
}>;
