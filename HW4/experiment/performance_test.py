#!/usr/bin/env python3

import requests
import json
import time
import matplotlib.pyplot as plt
import numpy as np
import concurrent.futures
from typing import List, Dict
import argparse

class PerformanceTester:
    def __init__(self, splitter_url: str, mapper_urls: List[str], reducer_url: str, bucket_name: str):
        self.splitter_url = splitter_url
        self.mapper_urls = mapper_urls
        self.reducer_url = reducer_url
        self.bucket_name = bucket_name
        
    def test_scalability(self, input_file: str, max_mappers: int = 10) -> Dict:
        """Test how performance scales with number of mappers"""
        results = {
            'num_mappers': [],
            'total_time': [],
            'map_time': [],
            'split_time': [],
            'reduce_time': []
        }
        
        print("Testing MapReduce Scalability (PARALLEL VERSION)")
        print("=" * 60)
        
        for num_mappers in range(1, min(max_mappers + 1, len(self.mapper_urls) + 1)):
            print(f"\nTest with {num_mappers} mapper(s)...")
            
            mapper_subset = self.mapper_urls[:num_mappers]
            start_time = time.time()
            
            # Split
            split_start = time.time()
            chunk_urls = self.split(input_file, num_mappers)
            split_time = time.time() - split_start
            
            # Map (PARALLEL!)
            map_start = time.time()
            result_urls = self.map_parallel(chunk_urls, mapper_subset)
            map_time = time.time() - map_start
            
            # Reduce
            reduce_start = time.time()
            final_result = self.reduce(result_urls)
            reduce_time = time.time() - reduce_start
            
            total_time = time.time() - start_time
            
            results['num_mappers'].append(num_mappers)
            results['total_time'].append(total_time)
            results['map_time'].append(map_time)
            results['split_time'].append(split_time)
            results['reduce_time'].append(reduce_time)
            
            print(f"  Split: {split_time:.2f}s, Map: {map_time:.2f}s (PARALLEL), Reduce: {reduce_time:.2f}s, Total: {total_time:.2f}s")
        
        return results
    
    def split(self, input_file: str, num_chunks: int) -> List[str]:
        url = f"{self.splitter_url}/split"
        payload = {
            "s3_url": input_file,
            "bucket_name": self.bucket_name,
            "num_chunks": num_chunks
        }
        response = requests.post(url, json=payload, timeout=60)
        response.raise_for_status()
        return response.json()['chunk_urls']
    
    def map_parallel(self, chunk_urls: List[str], mapper_urls: List[str]) -> List[str]:
        """Map chunks in PARALLEL using ThreadPoolExecutor"""
        def process_chunk(args):
            i, chunk_url = args
            mapper_url = mapper_urls[i % len(mapper_urls)]
            mapper_id = f"mapper_{i}"
            
            url = f"{mapper_url}/map"
            payload = {
                "chunk_url": chunk_url,
                "bucket_name": self.bucket_name,
                "mapper_id": mapper_id
            }
            response = requests.post(url, json=payload, timeout=60)
            response.raise_for_status()
            return response.json()['result_url']
        
        # Process all chunks in parallel
        with concurrent.futures.ThreadPoolExecutor(max_workers=len(chunk_urls)) as executor:
            futures = [executor.submit(process_chunk, (i, chunk_url)) 
                      for i, chunk_url in enumerate(chunk_urls)]
            result_urls = [future.result() for future in concurrent.futures.as_completed(futures)]
        
        return result_urls
    
    def reduce(self, result_urls: List[str]) -> Dict:
        url = f"{self.reducer_url}/reduce"
        payload = {
            "result_urls": result_urls,
            "bucket_name": self.bucket_name
        }
        response = requests.post(url, json=payload, timeout=60)
        response.raise_for_status()
        return response.json()
    
    def plot_scalability(self, results: Dict, output_file: str = 'scalability_parallel.png'):
        """Create visualization of scalability results"""
        fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(14, 5))
        
        # Plot 1: Total time vs number of mappers
        ax1.plot(results['num_mappers'], results['total_time'], 'o-', linewidth=2, markersize=8, label='Total Time', color='#2E86AB')
        ax1.plot(results['num_mappers'], results['map_time'], 's-', linewidth=2, markersize=6, label='Map Time (Parallel)', color='#06A77D')
        ax1.set_xlabel('Number of Mappers', fontsize=12)
        ax1.set_ylabel('Time (seconds)', fontsize=12)
        ax1.set_title('MapReduce Execution Time vs Parallelism\n(FIXED - Parallel Orchestration)', fontsize=14, fontweight='bold')
        ax1.legend()
        ax1.grid(True, alpha=0.3)
        
        # Plot 2: Speedup factor
        baseline_time = results['total_time'][0]
        speedup = [baseline_time / t for t in results['total_time']]
        ideal_speedup = results['num_mappers']
        
        ax2.plot(results['num_mappers'], speedup, 'o-', linewidth=2, markersize=8, label='Actual Speedup', color='#2E86AB')
        ax2.plot(results['num_mappers'], ideal_speedup, '--', linewidth=2, alpha=0.5, label='Ideal Speedup', color='#F18F01')
        ax2.set_xlabel('Number of Mappers', fontsize=12)
        ax2.set_ylabel('Speedup Factor', fontsize=12)
        ax2.set_title('Parallel Efficiency\n(FIXED - With ThreadPoolExecutor)', fontsize=14, fontweight='bold')
        ax2.legend()
        ax2.grid(True, alpha=0.3)
        
        plt.tight_layout()
        plt.savefig(output_file, dpi=300, bbox_inches='tight')
        print(f"\n✓ Scalability plot saved to {output_file}")
        
    def plot_breakdown(self, results: Dict, output_file: str = 'breakdown_parallel.png'):
        """Create stacked bar chart of time breakdown"""
        fig, ax = plt.subplots(figsize=(10, 6))
        
        x = np.arange(len(results['num_mappers']))
        width = 0.6
        
        p1 = ax.bar(x, results['split_time'], width, label='Split', color='#2E86AB')
        p2 = ax.bar(x, results['map_time'], width, bottom=results['split_time'], label='Map (Parallel)', color='#06A77D')
        
        bottom = [s + m for s, m in zip(results['split_time'], results['map_time'])]
        p3 = ax.bar(x, results['reduce_time'], width, bottom=bottom, label='Reduce', color='#D81159')
        
        ax.set_xlabel('Number of Mappers', fontsize=12)
        ax.set_ylabel('Time (seconds)', fontsize=12)
        ax.set_title('MapReduce Phase Breakdown\n(FIXED - Parallel Orchestration)', fontsize=14, fontweight='bold')
        ax.set_xticks(x)
        ax.set_xticklabels([str(n) for n in results['num_mappers']])
        ax.legend()
        ax.grid(True, alpha=0.3, axis='y')
        
        plt.tight_layout()
        plt.savefig(output_file, dpi=300, bbox_inches='tight')
        print(f"✓ Breakdown plot saved to {output_file}")
    
    def plot_comparison(self, old_results: Dict, new_results: Dict, output_file: str = 'comparison.png'):
        """Compare old sequential vs new parallel performance"""
        fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(14, 5))
        
        # Plot 1: Total time comparison
        x = np.arange(len(new_results['num_mappers']))
        width = 0.35
        
        ax1.bar(x - width/2, old_results['total_time'], width, label='Sequential (Old)', color='#D81159', alpha=0.7)
        ax1.bar(x + width/2, new_results['total_time'], width, label='Parallel (Fixed)', color='#06A77D', alpha=0.7)
        
        ax1.set_xlabel('Number of Mappers', fontsize=12)
        ax1.set_ylabel('Total Time (seconds)', fontsize=12)
        ax1.set_title('Performance Comparison: Sequential vs Parallel', fontsize=14, fontweight='bold')
        ax1.set_xticks(x)
        ax1.set_xticklabels([str(n) for n in new_results['num_mappers']])
        ax1.legend()
        ax1.grid(True, alpha=0.3, axis='y')
        
        # Plot 2: Speedup comparison
        old_baseline = old_results['total_time'][0]
        new_baseline = new_results['total_time'][0]
        old_speedup = [old_baseline / t for t in old_results['total_time']]
        new_speedup = [new_baseline / t for t in new_results['total_time']]
        
        ax2.plot(new_results['num_mappers'], old_speedup, 'o--', linewidth=2, markersize=8, 
                label='Sequential (Old)', color='#D81159', alpha=0.7)
        ax2.plot(new_results['num_mappers'], new_speedup, 'o-', linewidth=2, markersize=8, 
                label='Parallel (Fixed)', color='#06A77D')
        ax2.plot(new_results['num_mappers'], new_results['num_mappers'], ':', linewidth=2, 
                label='Ideal', color='#F18F01', alpha=0.5)
        
        ax2.set_xlabel('Number of Mappers', fontsize=12)
        ax2.set_ylabel('Speedup Factor', fontsize=12)
        ax2.set_title('Speedup Comparison', fontsize=14, fontweight='bold')
        ax2.legend()
        ax2.grid(True, alpha=0.3)
        
        plt.tight_layout()
        plt.savefig(output_file, dpi=300, bbox_inches='tight')
        print(f"✓ Comparison plot saved to {output_file}")


def main():
    parser = argparse.ArgumentParser(description='MapReduce Performance Tester (PARALLEL)')
    parser.add_argument('--splitter', required=True, help='Splitter service URL')
    parser.add_argument('--mappers', required=True, nargs='+', help='Mapper service URLs')
    parser.add_argument('--reducer', required=True, help='Reducer service URL')
    parser.add_argument('--bucket', required=True, help='S3 bucket name')
    parser.add_argument('--input', required=True, help='Input file S3 key')
    parser.add_argument('--max-mappers', type=int, default=10, help='Maximum mappers to test')
    
    args = parser.parse_args()
    
    print("\n🚀 Using PARALLEL orchestration with ThreadPoolExecutor")
    print("   This should show MUCH better scalability!\n")
    
    tester = PerformanceTester(
        splitter_url=args.splitter,
        mapper_urls=args.mappers,
        reducer_url=args.reducer,
        bucket_name=args.bucket
    )
    
    # Run scalability test
    results = tester.test_scalability(args.input, args.max_mappers)
    
    # Create visualizations
    tester.plot_scalability(results, 'mapreduce_scalability_parallel.png')
    tester.plot_breakdown(results, 'mapreduce_breakdown_parallel.png')
    
    # Print summary
    print("\n" + "=" * 60)
    print("Performance Testing Complete!")
    print("=" * 60)
    print("\nResults Summary:")
    for i, num_mappers in enumerate(results['num_mappers']):
        total = results['total_time'][i]
        speedup = results['total_time'][0] / total
        efficiency = (speedup / num_mappers) * 100
        print(f"  {num_mappers} mapper(s): {total:.2f}s (speedup: {speedup:.2f}x, efficiency: {efficiency:.1f}%)")


if __name__ == '__main__':
    main()