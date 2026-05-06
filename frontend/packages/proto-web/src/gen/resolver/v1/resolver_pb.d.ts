import type { GenFile, GenMessage, GenService } from "@bufbuild/protobuf/codegenv1";
import type { Message } from "@bufbuild/protobuf";
/**
 * Describes the file resolver/v1/resolver.proto.
 */
export declare const file_resolver_v1_resolver: GenFile;
/**
 * @generated from message resolver.v1.ResolveRequest
 */
export type ResolveRequest = Message<"resolver.v1.ResolveRequest"> & {
    /**
     * @generated from field: string code = 1;
     */
    code: string;
    /**
     * @generated from field: string user_agent = 2;
     */
    userAgent: string;
    /**
     * @generated from field: string referer = 3;
     */
    referer: string;
    /**
     * @generated from field: string ip = 4;
     */
    ip: string;
};
/**
 * Describes the message resolver.v1.ResolveRequest.
 * Use `create(ResolveRequestSchema)` to create a new message.
 */
export declare const ResolveRequestSchema: GenMessage<ResolveRequest>;
/**
 * @generated from message resolver.v1.ResolveResponse
 */
export type ResolveResponse = Message<"resolver.v1.ResolveResponse"> & {
    /**
     * @generated from field: string long_url = 1;
     */
    longUrl: string;
};
/**
 * Describes the message resolver.v1.ResolveResponse.
 * Use `create(ResolveResponseSchema)` to create a new message.
 */
export declare const ResolveResponseSchema: GenMessage<ResolveResponse>;
/**
 * @generated from service resolver.v1.ResolverService
 */
export declare const ResolverService: GenService<{
    /**
     * Resolve returns the long URL for a given code and emits a ClickRecorded event asynchronously.
     *
     * @generated from rpc resolver.v1.ResolverService.Resolve
     */
    resolve: {
        methodKind: "unary";
        input: typeof ResolveRequestSchema;
        output: typeof ResolveResponseSchema;
    };
}>;
