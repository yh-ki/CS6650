#!/usr/bin/env python3

import requests
import json
import time
import sys
import argparse
import concurrent.futures
from typing import List, Dict

class MapReduceOrchestrator:
    def __init__(self, splitter_url: str, mapper_urls: List[str], reducer_url: str, bucket_name: str):
        self.splitter_url = splitter_url
        self.mapper_urls = mapper_urls
        self.reducer_url = reducer_url
        self.bucket_name = bucket_name
        self.stats = {
            'split_time': 0,
            'map_time': 0,
            'reduce_time': 0,
            'total_time': 0
        }

    def run(self, input_file: str) -> Dict:
        """Run the complete MapReduce workflow"""
        start_time = time.time()
        
        print("=" * 60)
        print("Starting MapReduce Word Count (PARALLEL VERSION)")
        print("=" * 60)
        
        # Step 1: Split
        print(f"\n[1/3] Splitting file into {len(self.mapper_urls)} chunks...")
        split_start = time.time()
        chunk_urls = self.split(input_file)
        self.stats['split_time'] = time.time() - split_start
        print(f"✓ Split complete in {self.stats['split_time']:.2f}s")
        print(f"  Created {len(chunk_urls)} chunks")
        
        # Step 2: Map (PARALLEL!)
        print(f"\n[2/3] Mapping {len(chunk_urls)} chunks IN PARALLEL...")
        map_start = time.time()
        result_urls = self.map_parallel(chunk_urls)
        self.stats['map_time'] = time.time() - map_start
        print(f"✓ Map complete in {self.stats['map_time']:.2f}s")
        print(f"  Processed {len(result_urls)} chunks")
        
        # Step 3: Reduce
        print(f"\n[3/3] Reducing {len(result_urls)} results...")
        reduce_start = time.time()
        final_result = self.reduce(result_urls)
        self.stats['reduce_time'] = time.time() - reduce_start
        print(f"✓ Reduce complete in {self.stats['reduce_time']:.2f}s")
        
        self.stats['total_time'] = time.time() - start_time
        
        # Print results
        print("\n" + "=" * 60)
        print("MapReduce Complete!")
        print("=" * 60)
        print(f"\nResults:")
        print(f"  Final output: {final_result['final_url']}")
        print(f"  Total words: {final_result['total_words']:,}")
        print(f"  Unique words: {final_result['unique_words']:,}")
        print(f"\nTop 10 words:")
        for i, word_count in enumerate(final_result['top_words'], 1):
            print(f"  {i:2d}. {word_count['word']:15s} - {word_count['count']:,} occurrences")
        
        print(f"\nPerformance:")
        print(f"  Split time:  {self.stats['split_time']:7.2f}s")
        print(f"  Map time:    {self.stats['map_time']:7.2f}s (PARALLEL)")
        print(f"  Reduce time: {self.stats['reduce_time']:7.2f}s")
        print(f"  Total time:  {self.stats['total_time']:7.2f}s")
        
        return final_result

    def split(self, input_file: str) -> List[str]:
        """Split the input file into chunks"""
        url = f"{self.splitter_url}/split"
        payload = {
            "s3_url": input_file,
            "bucket_name": self.bucket_name,
            "num_chunks": len(self.mapper_urls)
        }
        
        response = requests.post(url, json=payload, timeout=60)
        response.raise_for_status()
        
        result = response.json()
        return result['chunk_urls']

    def map_parallel(self, chunk_urls: List[str]) -> List[str]:
        """
        Map chunks in PARALLEL using ThreadPoolExecutor
        This is the key fix for performance!
        """
        def process_chunk(args):
            """Process a single chunk (called in parallel)"""
            chunk_index, chunk_url = args
            mapper_url = self.mapper_urls[chunk_index % len(self.mapper_urls)]
            mapper_id = f"mapper_{chunk_index}"
            
            print(f"  [Thread {chunk_index}] Mapping chunk {chunk_index + 1}/{len(chunk_urls)} with {mapper_id}...")
            
            url = f"{mapper_url}/map"
            payload = {
                "chunk_url": chunk_url,
                "bucket_name": self.bucket_name,
                "mapper_id": mapper_id
            }
            
            try:
                response = requests.post(url, json=payload, timeout=60)
                response.raise_for_status()
                result = response.json()
                
                print(f"    [Thread {chunk_index}] ✓ Processed {result['word_count']} words")
                return result['result_url']
            except Exception as e:
                print(f"    [Thread {chunk_index}] ✗ Error: {e}")
                raise
        
        # Use ThreadPoolExecutor for parallel HTTP requests
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(chunk_urls)) as executor:
            # Submit all tasks at once
            futures = [
                executor.submit(process_chunk, (i, chunk_url))
                for i, chunk_url in enumerate(chunk_urls)
            ]
            
            # Wait for all to complete and collect results
            result_urls = []
            for future in concurrent.futures.as_completed(futures):
                try:
                    result_url = future.result()
                    result_urls.append(result_url)
                except Exception as e:
                    print(f"Mapper task failed: {e}")
                    raise
        
        return result_urls

    def reduce(self, result_urls: List[str]) -> Dict:
        """Reduce all mapper results into final word counts"""
        url = f"{self.reducer_url}/reduce"
        payload = {
            "result_urls": result_urls,
            "bucket_name": self.bucket_name
        }
        
        response = requests.post(url, json=payload, timeout=60)
        response.raise_for_status()
        
        return response.json()

    def get_stats(self) -> Dict:
        """Get performance statistics"""
        return self.stats


def main():
    parser = argparse.ArgumentParser(description='MapReduce Word Count Orchestrator (PARALLEL)')
    parser.add_argument('--splitter', required=True, help='Splitter service URL')
    parser.add_argument('--mappers', required=True, nargs='+', help='Mapper service URLs')
    parser.add_argument('--reducer', required=True, help='Reducer service URL')
    parser.add_argument('--bucket', required=True, help='S3 bucket name')
    parser.add_argument('--input', required=True, help='Input file S3 key')
    
    args = parser.parse_args()
    
    print("\n🚀 Using PARALLEL orchestration with ThreadPoolExecutor")
    print(f"   Mappers will process chunks concurrently!\n")
    
    orchestrator = MapReduceOrchestrator(
        splitter_url=args.splitter,
        mapper_urls=args.mappers,
        reducer_url=args.reducer,
        bucket_name=args.bucket
    )
    
    try:
        result = orchestrator.run(args.input)
        print("\n✓ Success!")
        return 0
    except Exception as e:
        print(f"\n✗ Error: {e}", file=sys.stderr)
        import traceback
        traceback.print_exc()
        return 1


if __name__ == '__main__':
    sys.exit(main())