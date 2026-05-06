import type { GenFile, GenMessage, GenService } from "@bufbuild/protobuf/codegenv1";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type { Message } from "@bufbuild/protobuf";
/**
 * Describes the file analytics/v1/analytics.proto.
 */
export declare const file_analytics_v1_analytics: GenFile;
/**
 * @generated from message analytics.v1.StatsRequest
 */
export type StatsRequest = Message<"analytics.v1.StatsRequest"> & {
    /**
     * @generated from field: string code = 1;
     */
    code: string;
    /**
     * Inclusive ISO date string YYYY-MM-DD; empty means "no lower bound".
     *
     * @generated from field: string from_date = 2;
     */
    fromDate: string;
    /**
     * Inclusive ISO date string YYYY-MM-DD; empty means "no upper bound".
     *
     * @generated from field: string to_date = 3;
     */
    toDate: string;
};
/**
 * Describes the message analytics.v1.StatsRequest.
 * Use `create(StatsRequestSchema)` to create a new message.
 */
export declare const StatsRequestSchema: GenMessage<StatsRequest>;
/**
 * @generated from message analytics.v1.StatsResponse
 */
export type StatsResponse = Message<"analytics.v1.StatsResponse"> & {
    /**
     * @generated from field: string code = 1;
     */
    code: string;
    /**
     * @generated from field: int64 total = 2;
     */
    total: bigint;
    /**
     * @generated from field: google.protobuf.Timestamp last_clicked_at = 3;
     */
    lastClickedAt?: Timestamp;
    /**
     * @generated from field: repeated analytics.v1.DailyCount daily = 4;
     */
    daily: DailyCount[];
};
/**
 * Describes the message analytics.v1.StatsResponse.
 * Use `create(StatsResponseSchema)` to create a new message.
 */
export declare const StatsResponseSchema: GenMessage<StatsResponse>;
/**
 * @generated from message analytics.v1.DailyCount
 */
export type DailyCount = Message<"analytics.v1.DailyCount"> & {
    /**
     * Date in YYYY-MM-DD (UTC).
     *
     * @generated from field: string date = 1;
     */
    date: string;
    /**
     * @generated from field: int64 count = 2;
     */
    count: bigint;
};
/**
 * Describes the message analytics.v1.DailyCount.
 * Use `create(DailyCountSchema)` to create a new message.
 */
export declare const DailyCountSchema: GenMessage<DailyCount>;
/**
 * @generated from service analytics.v1.AnalyticsService
 */
export declare const AnalyticsService: GenService<{
    /**
     * Stats returns aggregated click stats for a code.
     *
     * @generated from rpc analytics.v1.AnalyticsService.Stats
     */
    stats: {
        methodKind: "unary";
        input: typeof StatsRequestSchema;
        output: typeof StatsResponseSchema;
    };
}>;
